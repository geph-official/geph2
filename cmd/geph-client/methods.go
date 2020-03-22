package main

import (
	"crypto/rand"
	"errors"
	"fmt"
	"net"
	"strings"
	"sync"
	"time"

	"github.com/ethereum/go-ethereum/rlp"
	"github.com/geph-official/geph2/libs/bdclient"
	"github.com/geph-official/geph2/libs/cshirt2"
	"github.com/geph-official/geph2/libs/fastudp"
	"github.com/geph-official/geph2/libs/kcp-go"
	"github.com/geph-official/geph2/libs/niaucchi4"
	log "github.com/sirupsen/logrus"
	"github.com/xtaci/smux"
)

func getSingleHop(host string, pk []byte) (ss *smux.Session, err error) {
	var conn net.Conn
	if useTCP {
		conn, err = net.Dial("tcp", host)
		if err != nil {
			return
		}
		conn, err = cshirt2.Client(pk, conn)
		if err != nil {
			log.Warnln("cshirt2 failed:", err)
			return
		}
		ss, err = negotiateSmux(nil, conn, pk)
		return
	}
	conn, err = niaucchi4.DialKCP(host, pk)
	if err != nil {
		err = fmt.Errorf("plain TCP failed: %w", err)
		return
	}
	ss, err = negotiateSmux(nil, conn, pk)
	return
}

func getDirect(greeting [2][]byte, host string, pk []byte) (ss *smux.Session, err error) {
	var conn net.Conn
	if useTCP {
		conn, err = net.Dial("tcp", host+":2389")
		if err != nil {
			return
		}
		ss, err = negotiateSmux(&greeting, conn, pk)
		return
	}
	conn, err = niaucchi4.DialKCP(host+":2389", make([]byte, 32))
	if err != nil {
		err = fmt.Errorf("plain TCP failed: %w", err)
		return
	}
	ss, err = negotiateSmux(&greeting, conn, pk)
	return
}

func getSingleTCP(bridges []bdclient.BridgeInfo) (conn net.Conn, err error) {
	bridgeRace := make(chan net.Conn)
	bridgeDeadWait := new(sync.WaitGroup)
	bridgeDeadWait.Add(len(bridges))
	go func() {
		bridgeDeadWait.Wait()
		close(bridgeRace)
	}()
	syncer := make(chan bool)
	go func() {
		time.Sleep(time.Second)
		close(syncer)
	}()
	for _, bi := range bridges {
		bi := bi
		go func() {
			defer bridgeDeadWait.Done()
			bridgeConn, err := dialBridge(bi.Host, bi.Cookie)
			if err != nil {
				log.Debugln("dialing to", bi.Host, "failed!", err)
				return
			}
			bridgeConn.SetDeadline(time.Now().Add(time.Second * 30))
			rlp.Encode(bridgeConn, "conn/feedback")
			rlp.Encode(bridgeConn, exitName)
			_, err = bridgeConn.Read(make([]byte, 1))
			if err != nil {
				bridgeConn.Close()
				log.Debugln("conn in", bi.Host, "failed!", err)
				return
			}
			select {
			case bridgeRace <- bridgeConn:
				log.Infoln("Selected bridge", bridgeConn.RemoteAddr())
			default:
				bridgeConn.Close()
			}
		}()
	}
	zz, ok := <-bridgeRace
	if !ok {
		err = errors.New("singlepath timed out")
		return
	}
	useStats(func(sc *stats) {
		sc.bridgeThunk = func() []niaucchi4.LinkInfo {
			sessions := make([]niaucchi4.LinkInfo, 1)
			sessions[0].RemoteIP = strings.Split(zz.RemoteAddr().String(), ":")[0]
			sessions[0].RecvCnt = -1
			return sessions
		}
	})
	conn = zz
	return
}

func getMultiUDP(bridges []bdclient.BridgeInfo) (conn net.Conn, err error) {
	bridgeRace := make(chan bool)
	bridgeDeadWait := new(sync.WaitGroup)
	bridgeDeadWait.Add(len(bridges))
	usocket := niaucchi4.Wrap(func() net.PacketConn {
		us, err := net.ListenPacket("udp", ":")
		if err != nil {
			panic(err)
		}
		return fastudp.NewConn(us.(*net.UDPConn))
	})
	cookie := make([]byte, 32)
	rand.Read(cookie)
	osocket := niaucchi4.ObfsListen(cookie, usocket, true)
	e2esid := niaucchi4.NewSessAddr()
	e2e := niaucchi4.NewE2EConn(osocket)
	go func() {
		bridgeDeadWait.Wait()
		close(bridgeRace)
	}()
	useStats(func(sc *stats) {
		sc.bridgeThunk = func() []niaucchi4.LinkInfo {
			sessions := e2e.DebugInfo()
			if len(sessions) < 1 {
				return nil
			}
			return sessions[0]
		}
	})
	log.Infoln("NON-LEGACY e2e!")
	for _, b := range bridges {
		host, err := net.ResolveUDPAddr("udp4", b.Host)
		if err != nil {
			continue
		}
		osocket.AddCookieException(host, b.Cookie)
		e2e.SetSessPath(e2esid, host)
		log.Debugln("adding ephemeral", host, "to our e2e")
	}
	fecsize := 16
	if noFEC {
		fecsize = 0
	}
	toret, err := kcp.NewConn2(e2esid, nil, fecsize, fecsize, e2e)
	if err != nil {
		panic(err)
	}
	toret.SetWindowSize(10000, 10000)
	toret.SetNoDelay(0, 100, 32, 0)
	toret.SetStreamMode(true)
	toret.SetMtu(1300)
	conn = toret
	return
}
