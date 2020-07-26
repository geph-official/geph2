package main

import (
	"errors"
	"net"
	"sync"
	"time"

	"github.com/ethereum/go-ethereum/rlp"
	"github.com/geph-official/geph2/libs/bdclient"
	"github.com/geph-official/geph2/libs/warpfront"
	log "github.com/sirupsen/logrus"
)

func connThroughBridge(bridgeConn net.Conn) (exitConn net.Conn, err error) {
	bridgeConn.SetDeadline(time.Now().Add(time.Second * 30))
	rlp.Encode(bridgeConn, "conn/feedback")
	rlp.Encode(bridgeConn, exitName)
	_, err = bridgeConn.Read(make([]byte, 1))
	if err != nil {
		bridgeConn.Close()
		//log.Debugln("conn in", bi.Host, "failed!", err)
		return
	}
	bridgeConn.SetDeadline(time.Time{})
	exitConn = bridgeConn
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
		time.Sleep(time.Second * 3)
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
			<-syncer
			realConn, err := connThroughBridge(bridgeConn)
			if err != nil {
				bridgeConn.Close()
				log.Debugln("conn in", bi.Host, "failed!", err)
				return
			}
			select {
			case bridgeRace <- realConn:
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

func getWarpfront(host2front map[string]string) (conn net.Conn, err error) {
	for host, front := range host2front {
		log.Println("> WF", host, front)
		rc, e := warpfront.Connect(cleanHTTPClient, front, host)
		if e != nil {
			err = e
			log.Debugf("WF failed 1/2 %v", e)
			continue
		}
		c, e := connThroughBridge(rc)
		if e != nil {
			log.Debugf("WF failed 2/2 %v", e)
			err = e
			continue
		}
		conn = c
		return
	}
	return
}
