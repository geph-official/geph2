package main

import (
	"net"
	"sync"
	"time"

	log "github.com/sirupsen/logrus"

	"github.com/ethereum/go-ethereum/rlp"
	"github.com/xtaci/smux"
)

type commandDialer interface {
	DialCmd(cmds ...string) (conn net.Conn, ok bool)
}

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
	retval := make(chan net.Conn)
	go func() {
	start:
		sess := sw.fixSess()
		// markSessionNil marks the session nil only if it hasn't already been changed
		markSessionNil := func() {
			time.Sleep(time.Second)
			sw.lock.Lock()
			if sw.session == sess {
				sw.session = nil
			}
			sw.lock.Unlock()
		}
		strm, err := sess.OpenStream()
		if err != nil {
			sess.Close()
			log.Warnln(cmds, "can't open stream, trying again", err)
			markSessionNil()
			time.Sleep(time.Second)
			goto start
		}
		// dial command
		rlp.Encode(strm, cmds)
		strm.SetDeadline(time.Now().Add(time.Second * 8))
		// wait for response
		var connected bool
		err = rlp.Decode(strm, &connected)
		if err != nil {
			sess.Close()
			markSessionNil()
			log.Warnln(cmds, "can't read response, trying again:", err)
			time.Sleep(time.Second)
			goto start
		}
		select {
		case retval <- strm:
		default:
			log.Warnln(cmds, "closing late stream. This is BAAAAAD")
			strm.Close()
		}
	}()
	select {
	case conn = <-retval:
		conn.SetDeadline(time.Now().Add(time.Hour))
		ok = true
		return
	}
}
