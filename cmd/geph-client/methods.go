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
	"github.com/geph-official/geph2/libs/kcp-go"
	"github.com/geph-official/geph2/libs/niaucchi4"
	log "github.com/sirupsen/logrus"
	"github.com/xtaci/smux"
)

func getDirect(greeting [2][]byte, host string, pk []byte) (ss *smux.Session, err error) {
	var conn net.Conn
	if useTCP {
		conn, err = net.Dial("tcp", host+":2389")
		if err != nil {
			return
		}
		ss, err = negotiateSmux(greeting, conn, pk)
		return
	}
	conn, err = niaucchi4.DialKCP(host+":2389", make([]byte, 32))
	if err != nil {
		err = fmt.Errorf("plain TCP failed: %w", err)
		return
	}
	ss, err = negotiateSmux(greeting, conn, pk)
	return
}

func getSinglepath(bridges []bdclient.BridgeInfo) (conn net.Conn, err error) {
	bridgeRace := make(chan net.Conn)
	bridgeDeadWait := new(sync.WaitGroup)
	bridgeDeadWait.Add(len(bridges))
	go func() {
		bridgeDeadWait.Wait()
		close(bridgeRace)
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
			rlp.Encode(bridgeConn, "conn")
			rlp.Encode(bridgeConn, exitName)
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
	conn = zz
	return
}

func getMultipath(bridges []bdclient.BridgeInfo) (conn net.Conn, err error) {
	bridgeRace := make(chan bool)
	bridgeDeadWait := new(sync.WaitGroup)
	bridgeDeadWait.Add(len(bridges))
	usocket := niaucchi4.Wrap(func() net.PacketConn {
		us, err := net.ListenPacket("udp", ":")
		if err != nil {
			panic(err)
		}
		return us
	})
	cookie := make([]byte, 32)
	rand.Read(cookie)
	osocket := niaucchi4.ObfsListen(cookie, usocket)
	e2esid := niaucchi4.NewSessAddr()
	e2e := niaucchi4.NewE2EConn(osocket)
	go func() {
		bridgeDeadWait.Wait()
		close(bridgeRace)
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
			defer bridgeConn.Close()
			bridgeConn.SetDeadline(time.Now().Add(time.Second * 30))
			rlp.Encode(bridgeConn, "conn/e2e")
			rlp.Encode(bridgeConn, exitName)
			rlp.Encode(bridgeConn, cookie)
			var port uint
			e := rlp.Decode(bridgeConn, &port)
			if e != nil {
				log.Warnln("conn/e2e to", bi.Host, "failed:", e)
				return
			}
			complete := fmt.Sprintf("%v:%v", strings.Split(bi.Host, ":")[0], port)
			compudp, e := net.ResolveUDPAddr("udp", complete)
			if e != nil {
				log.Println("cannot resolve udp for", complete, err)
				return
			}
			log.Debugln("adding", complete, "to our e2e")
			e2e.SetSessPath(e2esid, compudp)
			select {
			case bridgeRace <- true:
			default:
			}
		}()
	}
	// get the bridge
	_, ok := <-bridgeRace
	if !ok {
		err = errors.New("e2e did not work")
		return
	}
	toret, err := kcp.NewConn2(e2esid, nil, 0, 0, e2e)
	if err != nil {
		panic(err)
	}
	toret.SetWindowSize(1000, 10000)
	toret.SetNoDelay(0, 50, 3, 0)
	toret.SetStreamMode(true)
	toret.SetMtu(1300)
	conn = toret
	return
}
