package main

import (
	"log"
	"net"
	"sync"
	"time"

	"github.com/ethereum/go-ethereum/rlp"
	"github.com/xtaci/smux"
)

type muxWrap struct {
	getSession func() *smux.Session

	lock    sync.Mutex
	session *smux.Session
}

func (sw *muxWrap) fixSess() *smux.Session {
	sw.lock.Lock()
	defer sw.lock.Unlock()
	if sw.session == nil {
		sw.session = sw.getSession()
	}
	return sw.session
}

func (sw *muxWrap) DialCmd(cmds ...string) (conn net.Conn, ok bool) {
start:
	sess := sw.fixSess()
	timeout := time.After(time.Second * 20)
	cancel := make(chan bool)
	go func() {
		select {
		case <-cancel:
			return
		case <-timeout:
			log.Println("timed out, deleting session")
			sess.Close()
		}

	}()
	strm, err := sess.OpenStream()
	if err != nil {
		sess.Close()
		sw.lock.Lock()
		sw.session = nil
		sw.lock.Unlock()
		log.Println("can't open stream, trying again")
		goto start
	}
	// dial command
	rlp.Encode(strm, cmds)
	// wait for response
	var connected bool
	err = rlp.Decode(strm, &connected)
	if err != nil {
		sess.Close()
		sw.lock.Lock()
		sw.session = nil
		sw.lock.Unlock()
		log.Println("can't read response, trying again")
		goto start
	}
	close(cancel)
	return strm, connected
}
