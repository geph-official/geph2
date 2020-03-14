package niaucchi4

import (
	"encoding/binary"
	"errors"
	"log"
	"math"
	"net"
	"strings"
	"sync"
	"time"

	lru "github.com/hashicorp/golang-lru"
	"github.com/minio/highwayhash"
	"golang.org/x/time/rate"
)

type rtTracker struct {
	tab   map[uint64]int
	queue []uint64
}

func newRtTracker() *rtTracker {
	return &rtTracker{
		tab: make(map[uint64]int),
	}
}

func (rtt *rtTracker) add(k uint64, v int) {
	rtt.tab[k] = v
	rtt.queue = append(rtt.queue, k)
	if len(rtt.queue) > 1000 {
		oldest := rtt.queue[0]
		rtt.queue = rtt.queue[1:]
		delete(rtt.tab, oldest)
	}
}

func (rtt *rtTracker) get(k uint64) int {
	z, ok := rtt.tab[k]
	if !ok {
		return -1
	}
	return z
}

type e2eSession struct {
	remote        []net.Addr
	info          []*e2eLinkInfo
	sessid        SessionAddr
	rdqueue       [][]byte
	dupRateLimit  *rate.Limiter
	infoRateLimit *rate.Limiter
	lastSend      time.Time
	lastRemid     int
	recvDedup     *lru.Cache
	sendDedup     *rtTracker
	sendCallback  func(e2ePacket, net.Addr)

	lock sync.Mutex
}

func newSession(sessid [16]byte, sendCallback func(e2ePacket, net.Addr)) *e2eSession {
	cache, _ := lru.New(128)
	return &e2eSession{
		dupRateLimit:  rate.NewLimiter(10, 100),
		infoRateLimit: rate.NewLimiter(30, 100),
		recvDedup:     cache,
		sendDedup:     newRtTracker(),
		sessid:        sessid,
		sendCallback:  sendCallback,
	}
}

type e2eLinkInfo struct {
	sendsn  uint64
	acksn   uint64
	recvsn  uint64
	recvcnt uint64

	recvsnRecent  uint32
	recvcntRecent uint32

	recvWindow replayWindow

	txCount    uint64
	rtxCount   uint64
	remoteLoss float64
	checkTime  time.Time

	lastSendTime  time.Time
	lastProbeTime time.Time
	lastProbeSn   uint64
	lastPing      int64
	lastPingTime  time.Time

	lastRecvTime time.Time
}

func (el *e2eLinkInfo) getScore() float64 {
	// TODO send loss is what we actually need!
	// recvLoss := math.Max(0, 1.0-float64(el.recvcnt)/(1+float64(el.recvsn)))
	// return math.Max(float64(el.lastPing), float64(time.Since(el.lastSendTime).Milliseconds())) + recvLoss*100
	now := time.Now()
	if now.Sub(el.checkTime).Seconds() > 5 {
		// ensure accurate measurement
		if el.txCount > 1000 {
			el.rtxCount /= 2
			el.txCount /= 2
			el.checkTime = now
		}
	}
	pseudoPing := float64(el.lastPing) + math.Max(0, time.Since(el.lastRecvTime).Seconds()*1000-3000)
	loss := float64(el.rtxCount) / (float64(el.txCount) + 1)
	if el.remoteLoss >= 0 {
		loss = el.remoteLoss
	}
	if loss > 1 {
		loss = 1
	}
	return pseudoPing * (1 / (1.01 - math.Min(1, loss))) // intuition: expected retransmissions needed
	// return el.longLoss
}

type e2ePacket struct {
	Session SessionAddr
	Sn      uint64
	Ack     uint64
	Body    []byte
	Padding []byte
}

// LinkInfo describes info for a link.
type LinkInfo struct {
	RemoteIP      string
	RecvCnt       int
	Ping          int
	LossPct       float64
	RecentLossPct float64
	Score         float64
}

// DebugInfo dumps out info about all the links.
func (es *e2eSession) DebugInfo() (lii []LinkInfo) {
	es.lock.Lock()
	defer es.lock.Unlock()
	for i, nfo := range es.info {
		lii = append(lii, LinkInfo{
			RemoteIP:      strings.Split(es.remote[i].String(), ":")[0],
			RecvCnt:       int(nfo.recvcnt),
			Ping:          int(nfo.lastPing),
			LossPct:       math.Max(0, 1.0-float64(nfo.recvcnt)/(1+float64(nfo.recvsn))),
			RecentLossPct: math.Max(0, 1.0-float64(nfo.recvcntRecent)/(1+float64(nfo.recvsnRecent))),
			Score:         nfo.getScore(),
		})
	}
	return
}

func (es *e2eSession) AddPath(host net.Addr) {
	es.lock.Lock()
	defer es.lock.Unlock()
	for _, h := range es.remote {
		if h.String() == host.String() {
			return
		}
	}
	if doLogging {
		log.Printf("N4: [%p] adding new path %v", es, host)
	}
	es.remote = append(es.remote, host)
	es.info = append(es.info, &e2eLinkInfo{lastPing: 10000000, lastRecvTime: time.Now(), remoteLoss: -1})
}

func (es *e2eSession) processStats(pkt e2ePacket, remid int) {
	recvd := binary.LittleEndian.Uint32(pkt.Body[:4])
	total := binary.LittleEndian.Uint32(pkt.Body[4:])
	loss := 1 - float64(recvd)/(float64(total)+1)
	rid := es.info[remid]
	if rid.remoteLoss < 0 || (loss < rid.remoteLoss && total > 50) {
		rid.remoteLoss = loss
	} else {
		rid.remoteLoss = 0.9*rid.remoteLoss + 0.1*loss
	}
}

// Input processes a packet through the e2e session state.
func (es *e2eSession) Input(pkt e2ePacket, source net.Addr) {
	es.lock.Lock()
	defer es.lock.Unlock()
	sendInfo := func(remid int) {
		now := time.Now()
		nfo := es.info[remid]
		if nfo.recvsnRecent > 1000 && now.Sub(nfo.checkTime).Seconds() > 5 {
			nfo.recvsnRecent /= 2
			nfo.recvcntRecent /= 2
			nfo.checkTime = now
		}
		buf := make([]byte, 8)
		binary.LittleEndian.PutUint32(buf[4:], nfo.recvsnRecent)
		binary.LittleEndian.PutUint32(buf[:4], nfo.recvcntRecent)
		es.rawSend(false, remid, buf)
	}
	if pkt.Session != es.sessid {
		log.Println("pkt.Session =", pkt.Session, "; es.sessid =", es.sessid)
		panic("wrong sessid passed to Input")
	}
	// first find the remote
	remid := -1
	for i, v := range es.remote {
		if v.String() == source.String() {
			remid = i
			break
		}
	}
	if remid < 0 {
		if doLogging {
			log.Println("N4: e2eSession.Input() failed to find remid")
		}
		return
	}
	// parse the stuff
	if !es.info[remid].recvWindow.check(pkt.Sn) {
		if doLogging {
			log.Println("N4: discarding", pkt.Sn, "<", es.info[remid].recvsn)
		}
		return
	}
	if es.info[remid].recvsn < pkt.Sn {
		es.info[remid].recvsnRecent += uint32(pkt.Sn - es.info[remid].recvsn)
		es.info[remid].recvsn = pkt.Sn
		es.info[remid].acksn = pkt.Ack
	}

	es.info[remid].recvcnt++
	es.info[remid].recvcntRecent++
	if len(pkt.Body) > 8 {
		bodyHash := highwayhash.Sum128(pkt.Body, make([]byte, 32))
		if es.recvDedup.Contains(bodyHash) {
		} else {
			es.recvDedup.Add(bodyHash, true)
			es.rdqueue = append(es.rdqueue, pkt.Body)
		}
	}
	if len(pkt.Body) == 8 {
		es.processStats(pkt, remid)
	}
	nfo := es.info[remid]
	now := time.Now()
	if nfo.acksn > nfo.lastProbeSn && nfo.acksn > 10 {
		pingSample := now.Sub(nfo.lastProbeTime).Milliseconds()
		if pingSample < nfo.lastPing || now.Sub(nfo.lastPingTime).Seconds() > 60 {
			nfo.lastPing = pingSample
			nfo.lastPingTime = now
		}
		nfo.lastProbeSn = nfo.sendsn
		nfo.lastProbeTime = now
	}
	nfo.lastRecvTime = now

	if es.infoRateLimit.Allow() {
		sendInfo(remid)
	}
}

func (es *e2eSession) rawSend(rtd bool, remid int, payload []byte) {
	now := time.Now()
	if len(payload) > 1000 {
		bodyHash := highwayhash.Sum64(payload[256:], make([]byte, 32))
		val := es.sendDedup.get(bodyHash)
		if val >= 0 {
			//devalFactor := math.Pow(0.9, now.Sub(es.info[val].lastSendTime).Seconds())
			es.info[val].rtxCount++
			es.info[val].lastSendTime = now
		} else {
			//devalFactor := math.Pow(0.9, now.Sub(es.info[remid].lastSendTime).Seconds())
			es.sendDedup.add(bodyHash, remid)
			es.info[remid].txCount++
			es.info[remid].lastSendTime = now
		}
	}
	// create pkt
	toSend := e2ePacket{
		Session: es.sessid,
		Sn:      es.info[remid].sendsn,
		Ack:     es.info[remid].recvsn + 1,
		Body:    payload,
	}
	es.info[remid].sendsn++
	dest := es.remote[remid]
	es.sendCallback(toSend, dest)
}

// Send sends a packet. It returns instructions to where the packet should be sent etc
func (es *e2eSession) Send(payload []byte) (err error) {
	es.lock.Lock()
	defer es.lock.Unlock()
	now := time.Now()
	// find the right destination
	if es.dupRateLimit.AllowN(time.Now(), len(es.remote)) {
		//log.Println("sending small payload", len(payload), "to all paths")
		for remid := range es.remote {
			es.rawSend(false, remid, payload)
		}
	}
	remid := -1
	if time.Since(es.lastSend).Seconds() > 0.1 {
		lowPoint := -1.0
		for i, li := range es.info {
			if score := li.getScore(); score < lowPoint || lowPoint < 0 {
				lowPoint = score
				remid = i
			}
		}
		if doLogging {
			log.Println("N4: selected", es.remote[remid], "with score", es.info[remid].getScore())
			go func() {
				for remid, v := range es.DebugInfo() {
					log.Printf("%v %v %v/%v", v.RemoteIP, v.Ping,
						es.info[remid].rtxCount, es.info[remid].txCount)
				}
			}()
		}
		if remid == -1 {
			err = errors.New("cannot find any path")
			return
		}
		es.lastSend = now
	} else {
		remid = es.lastRemid
	}
	es.lastRemid = remid
	es.rawSend(true, remid, payload)
	return
}

// FlushReadQueue flushes the entire read queue.
func (es *e2eSession) FlushReadQueue(onPacket func([]byte)) {
	es.lock.Lock()
	defer es.lock.Unlock()
	for _, b := range es.rdqueue {
		onPacket(b)
	}
	es.rdqueue = es.rdqueue[:0]
}
