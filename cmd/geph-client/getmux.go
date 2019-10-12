package main

import (
	"crypto/ed25519"
	"errors"
	"fmt"
	"log"
	"net"
	"time"

	"github.com/ethereum/go-ethereum/rlp"
	"github.com/geph-official/geph2/libs/kcp-go"
	"github.com/geph-official/geph2/libs/niaucchi4"
	"github.com/geph-official/geph2/libs/tinyss"
	"github.com/xtaci/smux"
)

func getBridged(greeting [2][]byte, bridgeHost string, bridgeCookie []byte, exitName string, exitPK []byte) (ss *smux.Session, err error) {
	udpsock, err := net.ListenPacket("udp", "")
	if err != nil {
		panic(err)
	}
	obfssock := niaucchi4.ObfsListen(bridgeCookie, udpsock)
	kcpConn, err := kcp.NewConn(bridgeHost, nil, 0, 0, obfssock)
	if err != nil {
		obfssock.Close()
		return
	}
	kcpConn.SetWindowSize(10000, 10000)
	kcpConn.SetNoDelay(0, 40, 3, 0)
	kcpConn.SetStreamMode(true)
	kcpConn.SetMtu(1300)
	rlp.Encode(kcpConn, "conn")
	rlp.Encode(kcpConn, exitName)
	ss, err = negotiateSmux(greeting, kcpConn, exitPK)
	return
}

func getDirect(greeting [2][]byte, host string, pk []byte) (ss *smux.Session, err error) {
	kcpConn, err := niaucchi4.Dial(host+":2389", make([]byte, 32))
	if err != nil {
		err = fmt.Errorf("plain TCP failed: %w", err)
		return
	}
	ss, err = negotiateSmux(greeting, kcpConn, pk)
	return
}

func negotiateSmux(greeting [2][]byte, rawConn net.Conn, pk []byte) (ss *smux.Session, err error) {
	rawConn.SetDeadline(time.Now().Add(time.Second * 10))
	cryptConn, err := tinyss.Handshake(rawConn)
	if err != nil {
		err = fmt.Errorf("tinyss handshake failed: %w", err)
		rawConn.Close()
		return
	}
	// verify the actual msg
	var sssig []byte
	err = rlp.Decode(cryptConn, &sssig)
	if err != nil {
		err = fmt.Errorf("cannot decode sssig: %w", err)
		rawConn.Close()
		return
	}
	log.Println("crypto got")
	if !ed25519.Verify(pk, cryptConn.SharedSec(), sssig) {
		err = errors.New("man in the middle")
		rawConn.Close()
		return
	}
	// send the greeting
	rlp.Encode(cryptConn, greeting)
	log.Println("greeting sent")
	// wait for the reply
	var reply string
	err = rlp.Decode(cryptConn, &reply)
	if err != nil {
		err = fmt.Errorf("cannot decode reply: %w", err)
		rawConn.Close()
		return
	}
	if reply != "OK" {
		err = errors.New("authentication failed")
		rawConn.Close()
		return
	}
	ss, err = smux.Client(cryptConn, &smux.Config{
		KeepAliveInterval: time.Minute * 20,
		KeepAliveTimeout:  time.Minute * 22,
		MaxFrameSize:      10000,
		MaxReceiveBuffer:  1024 * 1024 * 100,
	})
	if err != nil {
		rawConn.Close()
		err = fmt.Errorf("smux error: %w", err)
		return
	}
	rawConn.SetDeadline(time.Time{})
	return
}
