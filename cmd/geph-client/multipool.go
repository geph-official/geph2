package main

import (
	"crypto/rand"
	"encoding/binary"
	"encoding/hex"
	"errors"
	"net"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/ethereum/go-ethereum/rlp"
	"github.com/geph-official/geph2/libs/backedtcp"
	"github.com/geph-official/geph2/libs/cshirt2"
	"github.com/geph-official/geph2/libs/tinysocks"
	pq "github.com/jupp0r/go-priority-queue"
	log "github.com/sirupsen/logrus"
	"github.com/xtaci/smux"
)

const mpSize = 6

type mpMember struct {
	session *smux.Session
	btcp    *backedtcp.Socket
	score   float64
}

type multipool struct {
	memberQueue     pq.PriorityQueue
	memberQueueCvar *sync.Cond
	metasess        [32]byte

	worstPing     time.Duration
	worstPingTime time.Time
	worstPingLock sync.Mutex
}

func (mp *multipool) popSession() mpMember {
	mp.memberQueueCvar.L.Lock()
	defer mp.memberQueueCvar.L.Unlock()
	// wait until there are elements
	for mp.memberQueue.Len() == 0 {
		mp.memberQueueCvar.Wait()
	}
	tr, err := mp.memberQueue.Pop()
	if err != nil {
		panic(err)
	}
	return tr.(mpMember)
}

func (mp *multipool) pushSession(sess mpMember) {
	mp.memberQueueCvar.L.Lock()
	defer mp.memberQueueCvar.L.Unlock()
	mp.memberQueue.Insert(sess, sess.score)
	mp.memberQueueCvar.Broadcast()
}

func (mp *multipool) getWorstPing() time.Duration {
	mp.worstPingLock.Lock()
	defer mp.worstPingLock.Unlock()
	return mp.worstPing
}

func (mp *multipool) setPing(d time.Duration) {
	mp.worstPingLock.Lock()
	defer mp.worstPingLock.Unlock()
	now := time.Now()
	if d > mp.worstPing || now.Sub(mp.worstPingTime).Seconds() > 10 {
		mp.worstPingTime = now
		mp.worstPing = d
	}
}

func newMultipool() *multipool {
	tr := &multipool{}
	tr.memberQueue = pq.New()
	tr.memberQueueCvar = sync.NewCond(&sync.Mutex{})
	rand.Read(tr.metasess[:])
	go func() {
		for i := 0; i < mpSize; i++ {
			tr.fillOne()
			time.Sleep(time.Second * 30)
		}
	}()
	tr.worstPing = time.Second * 5
	return tr
}

func (mp *multipool) fillOne() {
	var sessid [32]byte
	rand.Read(sessid[:])
	first := true
	getConn := func() (net.Conn, error) {
	retry:
		conn, err := getCleanConn()
		if err != nil {
			log.Println("failed getCleanConn():", err)
			time.Sleep(time.Second)
			goto retry
		}
		var greeting struct {
			MetaSess [32]byte
			SessID   [32]byte
		}
		greeting.MetaSess = mp.metasess
		greeting.SessID = sessid
		binary.Write(conn, binary.BigEndian, greeting)
		var response byte
		err = binary.Read(conn, binary.BigEndian, &response)
		if err != nil {
			log.Println("failed response read:", err)
			time.Sleep(time.Second)
			goto retry
		}
		log.Println("getConn to", conn.RemoteAddr(), "returned response", response)
		if !first && response != 1 {
			return nil, errors.New("bad")
		}
		first = false
		return conn, err
	}
	btcp := backedtcp.NewSocket(getConn)
	sm, err := smux.Client(btcp, &smux.Config{
		Version:           2,
		KeepAliveInterval: time.Minute * 20,
		KeepAliveTimeout:  time.Minute * 40,
		MaxFrameSize:      32768,
		MaxReceiveBuffer:  4 * 1024 * 1024,
		MaxStreamBuffer:   2 * 1024 * 1024,
	})
	if err != nil {
		panic(err)
	}
	mp.pushSession(mpMember{
		session: sm,
		btcp:    btcp,
		score:   0,
	})
}

func (mp *multipool) DialCmd(cmds ...string) (conn net.Conn, ok bool) {
	for {
		mem := mp.popSession()
		worst := mp.getWorstPing()
		// repeatedly reset the underlying connection
		success := make(chan bool)
		go func() {
			for {
				select {
				case <-time.After(time.Second * 10):
					log.Println("forcing replacement!")
					mem.btcp.Reset()
				case <-success:
					return
				}
			}
		}()
		start := time.Now()
		stream, err := mem.session.OpenStream()
		if err != nil {
			mem.session.Close()
			log.Println("error while opening stream, throwing away:", err.Error())
			close(success)
			go mp.fillOne()
			continue
		}
		rlp.Encode(stream, cmds)
		var connected bool
		stream.SetDeadline(time.Now().Add(time.Millisecond*time.Duration(worst) + time.Second*10))
		err = rlp.Decode(stream, &connected)
		close(success)
		if err != nil {
			mem.session.Close()
			log.Println("error while waiting for stream, throwing away:", err.Error())
			go mp.fillOne()
			continue
		}
		stream.SetDeadline(time.Time{})
		ping := time.Since(start)
		mp.setPing(ping)
		fping := float64(ping.Milliseconds())
		// compute ping
		if fping > mem.score {
			mem.score = 0.1*mem.score + 0.9*fping
		} else {
			mem.score = 0.5*mem.score + 0.5*fping
		}
		mp.pushSession(mem)
		return stream, true
	}
}

var cleanHTTPClient = &http.Client{
	Transport: &http.Transport{
		Proxy:           nil,
		IdleConnTimeout: time.Second * 120,
	},
	Timeout: time.Second * 120,
}

// get a clean, authenticated channel all the way to the exit
func getCleanConn() (conn net.Conn, err error) {
	var rawConn net.Conn
	if singleHop != "" {
		splitted := strings.Split(singleHop, "@")
		if len(splitted) != 2 {
			panic("-singleHop must be pk@host")
		}
		var tcpConn net.Conn
		var e error
		if upstreamProxy != "" {
			tcpConn, e = net.DialTimeout("tcp", upstreamProxy, time.Second*5)
			if e != nil {
				log.Warnln("failed to connect to SOCKS5 font proxy server:", e)
				err = e
				return
			}
			e, _ = tinysocks.Client(tcpConn, tinysocks.ParseAddr(splitted[1]), tinysocks.CmdConnect)
			if e != nil {
				tcpConn.Close()
				log.Warnln("failed handshake with second SOCKS5 server:", e)
				err = e
				return
			}
		} else {
			tcpConn, e = net.DialTimeout("tcp", splitted[1], time.Second*5)
			if e != nil {
				log.Warn("cannot connect to singleHop server:", e)
				err = e
				return
			}
		}
		pk, e := hex.DecodeString(splitted[0])
		if e != nil {
			panic(e)
		}
		obfsConn, e := cshirt2.Client(pk, tcpConn)
		if e != nil {
			log.Warn("cannot negotiate cshirt2 with singleHop server:", e)
			err = e
			return
		}
		cryptConn, e := negotiateTinySS(nil, obfsConn, pk, 'R')
		if e != nil {
			log.Warn("cannot negotiate tinyss with singleHop server:", e)
			err = e
			return
		}
		conn = cryptConn
		return
	}
	ubsig, ubmsg, err := getGreeting()
	if err != nil {
		return
	}

	if direct {
		if upstreamProxy != "" {
			rawConn, err = net.DialTimeout("tcp", upstreamProxy, time.Second*5)
			if err != nil {
				log.Warnln("failed to connect to singlehop server:", err)
				return
			}
			err, _ = tinysocks.Client(rawConn, tinysocks.ParseAddr(exitName+":2389"), tinysocks.CmdConnect)
			if err != nil {
				rawConn.Close()
				log.Warnln("failed handshake with second SOCKS5 server:", err)
				return
			}
		} else {
			rawConn, err = net.DialTimeout("tcp", exitName+":2389", time.Second*5)
			if err != nil {
				log.Warnln("failed to connect to exit server: %v", err)
				return
			}
		}
		if err == nil {
			rawConn.(*net.TCPConn).SetKeepAlive(false)
		}
	} else {
		getWarpfrontCon := func() (warpConn net.Conn, err error) {
			var wfstuff map[string]string
			wfstuff, err = bindClient.GetWarpfronts()
			if err != nil {
				log.Warnln("can't get warp front:", err)
				return
			}
			warpConn, err = getWarpfront(wfstuff)
			return
		}
		if forceWarpfront {
			rawConn, err = getWarpfrontCon()
			if err != nil {
				return
			}
		} else {
			bridges, e := getBridges(ubmsg, ubsig)
			if e != nil {
				err = e
				log.Warnln("getting bridges failed, retrying", err)
				return
			}
			rawConn, err = getSingleTCP(bridges)
			if err != nil {
				log.Warnf("can't connect to bridges (%v); time to W A R P F R O N T", err)
				rawConn, err = getWarpfrontCon()
				if err != nil {
					return
				}
			}
		}
	}
	rawConn.SetDeadline(time.Now().Add(time.Second * 10))
	cryptConn, err := negotiateTinySS(&[2][]byte{ubsig, ubmsg}, rawConn, exitPK(), 'R')
	if err != nil {
		log.Println("error while negotiating cryptConn", err)
		return
	}
	rawConn.SetDeadline(time.Time{})
	conn = cryptConn
	return
}

func exitPK() []byte {
	realExitKey, err := hex.DecodeString(exitKey)
	if err != nil {
		panic(err)
	}
	return realExitKey
}
