package main

import (
	"errors"
	"fmt"
	"net"
	"sync"
	"time"

	"github.com/ethereum/go-ethereum/rlp"
	"github.com/geph-official/geph2/libs/tinysocks"
	"github.com/geph-official/geph2/libs/warpfront"
	log "github.com/sirupsen/logrus"
)

type Bridge struct {
	Host    string
	Cookie  []byte
	IsSOCKS bool
}

func (b *Bridge) ShowInfo() {
	if b.IsSOCKS {
		log.Infof(".... SOCKS5 %v ", b.Host)
	} else {
		log.Infof(".... %v %x", b.Host, b.Cookie)
	}
}

func (b *Bridge) SetGephBridge(host string, cookie []byte) {
	b.IsSOCKS = false
	b.Host = host
	b.Cookie = cookie
}

func (b *Bridge) SetSOCKS5Bridge(host string) {
	b.IsSOCKS = true
	b.Host = host
}

func (b *Bridge) Conn() (net.Conn, error) {
	if b.IsSOCKS {
		rawconn, err := net.DialTimeout("tcp", b.Host, time.Second*5)
		if err != nil {
			log.Println("failed connect to second socks5 server:", err)
			return nil, err
		}
		target := exitName
		if singleHop == "" {
			target = fmt.Sprintf("%v:2389", exitName)
		}
		err, _ = tinysocks.Client(rawconn, tinysocks.ParseAddr(target), tinysocks.CmdConnect)
		if err != nil {
			log.Println("failed shakehand with second socks5 server:", err)
			rawconn.Close()
			return nil, err
		}
		return rawconn, err
	} else {
		bridgeConn, err := dialBridge(b.Host, b.Cookie)
		if err != nil {
			log.Debugln("dialing to", b.Host, "failed!", err)
			return nil, err
		}
		realConn, err := connThroughBridge(bridgeConn)
		if err != nil {
			bridgeConn.Close()
			log.Debugln("conn in", b.Host, "failed!", err)
			return nil, err
		}
		return realConn, err
	}
}

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

func getSingleTCP(bridges []Bridge) (conn net.Conn, err error) {
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
			realConn, err := bi.Conn()
			if err != nil {
				//log.Debugln("conn in", bi.Host, "failed!", err)
				return
			}
			select {
			case bridgeRace <- realConn:
			default:
				realConn.Close()
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
