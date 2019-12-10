package backedtcp

import (
	"encoding/binary"
	"fmt"
	"log"
	"net"
	"time"

	"github.com/patrickmn/go-cache"
)

type Listener struct {
	listen   net.Listener
	sessions *cache.Cache
	accepted chan *Socket
}

func Listen(listener net.Listener) *Listener {
	ls := &Listener{
		listen:   listener,
		sessions: cache.New(time.Hour, time.Hour),
		accepted: make(chan *Socket),
	}
	go func() {
		for {
			conn, err := ls.listen.Accept()
			if err != nil {
				continue
			}
			log.Println("got underlying socket")
			go func() {
				var sessid uint64
				err = binary.Read(conn, binary.BigEndian, &sessid)
				if err != nil {
					conn.Close()
					return
				}
				log.Println("got sessid", sessid)
				conn.SetDeadline(time.Now().Add(time.Hour))
				sessi, ok := ls.sessions.Get(fmt.Sprint(sessid))
				if !ok {
					log.Println("pushing out")
					sock := NewSocket(conn)
					ls.sessions.SetDefault(fmt.Sprint(sessid), sock)
					ls.accepted <- sock
				} else {
					ls.sessions.SetDefault(fmt.Sprint(sessid), sessi.(*Socket))
					sessi.(*Socket).Replace(conn)
				}
			}()
		}
	}()
	return ls
}

func (ls *Listener) Accept() (net.Conn, error) {
	conn := <-ls.accepted
	log.Println("accepted here")
	return conn, nil
}

type dummyAddr string

func (da dummyAddr) String() string {
	return string(da)
}

func (da dummyAddr) Network() string {
	return string(da)
}
