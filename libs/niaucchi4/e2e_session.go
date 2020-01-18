package niaucchi4

import (
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

type e2eSession struct {
	remote       []net.Addr
	info         []e2eLinkInfo
	sessid       SessionAddr
	rdqueue      [][]byte
	dupRateLimit *rate.Limiter
	lastSend     time.Time
	lastRemid    int
	recvDedup    *lru.Cache
	sendDedup    *lru.Cache

	lock sync.Mutex
}

func newSession(sessid [16]byte) *e2eSession {
	cache, _ := lru.New(128)
	zcache, _ := lru.New(1024)
	return &e2eSession{
		dupRateLimit: rate.NewLimiter(10*1000, 10*1000),
		recvDedup:    cache,
		sendDedup:    zcache,
		sessid:       sessid,
	}
}

type e2eLinkInfo struct {
	sendsn  uint64
	acksn   uint64
	recvsn  uint64
	recvcnt uint64

	longLoss float64

	lastSendTime time.Time
	lastSendSn   uint64
	lastPing     int64
}

func (el e2eLinkInfo) getScore() float64 {
	// TODO send loss is what we actually need!
	// recvLoss := math.Max(0, 1.0-float64(el.recvcnt)/(1+float64(el.recvsn)))
	// return math.Max(float64(el.lastPing), float64(time.Since(el.lastSendTime).Milliseconds())) + recvLoss*100
	factor := math.Max(float64(el.lastPing), float64(time.Since(el.lastSendTime).Milliseconds()))
	return factor + el.longLoss*2000
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
	RemoteIP string
	RecvCnt  int
	Ping     int
	LossPct  float64
}

// DebugInfo dumps out info about all the links.
func (es *e2eSession) DebugInfo() (lii []LinkInfo) {
	es.lock.Lock()
	defer es.lock.Unlock()
	for i, nfo := range es.info {
		lii = append(lii, LinkInfo{
			RemoteIP: strings.Split(es.remote[i].String(), ":")[0],
			RecvCnt:  int(nfo.recvcnt),
			Ping:     int(nfo.lastPing),
			LossPct:  math.Max(0, 1.0-float64(nfo.recvcnt)/(1+float64(nfo.recvsn))),
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
		log.Println("N4: adding new path", host)
	}
	es.remote = append(es.remote, host)
	es.info = append(es.info, e2eLinkInfo{lastPing: 10000000})
}

// Input processes a packet through the e2e session state.
func (es *e2eSession) Input(pkt e2ePacket, source net.Addr) {
	es.lock.Lock()
	defer es.lock.Unlock()
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
	if pkt.Sn < es.info[remid].recvsn {
		if pkt.Sn+16 < es.info[remid].recvsn {
			if doLogging {
				log.Println("N4: discarding", pkt.Sn, "<", es.info[remid].recvsn)
			}
			return
		}
	} else {
		es.info[remid].recvsn = pkt.Sn
		es.info[remid].acksn = pkt.Ack
	}

	es.info[remid].recvcnt++
	bodyHash := highwayhash.Sum128(pkt.Body, make([]byte, 32))
	if es.recvDedup.Contains(bodyHash) {
	} else {
		es.recvDedup.Add(bodyHash, true)
		es.rdqueue = append(es.rdqueue, pkt.Body)
	}
	nfo := &es.info[remid]
	if nfo.acksn > nfo.lastSendSn {
		now := time.Now()
		pingSample := now.Sub(nfo.lastSendTime).Milliseconds()
		if pingSample < nfo.lastPing {
			nfo.lastPing = pingSample
		} else {
			nfo.lastPing = 15*nfo.lastPing/16 + pingSample/16
		}
		nfo.lastSendSn = nfo.sendsn
		nfo.lastSendTime = now
	}
}

// Send sends a packet. It returns instructions to where the packet should be sent etc
func (es *e2eSession) Send(payload []byte, sendCallback func(e2ePacket, net.Addr)) (err error) {
	es.lock.Lock()
	defer es.lock.Unlock()
	send := func(rtd bool, remid int) {
		if rtd && len(payload) > 256 {
			bodyHash := highwayhash.Sum128(payload[256:], make([]byte, 32))
			val, ok := es.sendDedup.Get(bodyHash)
			if ok {
				es.info[val.(int)].longLoss =
					es.info[val.(int)].longLoss*0.999 + 0.001
			} else {
				es.sendDedup.Add(bodyHash, remid)
				es.info[remid].longLoss = es.info[remid].longLoss * 0.999
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
		sendCallback(toSend, dest)
	}
	now := time.Now()
	// find the right destination
	if es.dupRateLimit.AllowN(time.Now(), len(es.remote)*(len(payload)+50)) {
		for remid := range es.remote {
			send(false, remid)
		}
	} else {
		remid := -1
		if true {
			lowPoint := 1e20
			for i, li := range es.info {
				if score := li.getScore(); score < lowPoint {
					lowPoint = score
					remid = i
				}
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
		send(true, remid)
	}
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
