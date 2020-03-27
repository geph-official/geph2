package pseudotcp

import (
	"io"
	"log"
	"net"
	"sync"
	"time"

	"github.com/xtaci/smux"
	"gopkg.in/tomb.v1"
)

var connPool struct {
	locks  sync.Map // string => *sync.RWMutex
	smuxes sync.Map // string => *smux.Session
}

func getLock(host string) *sync.RWMutex {
	lok, _ := connPool.locks.LoadOrStore(host, new(sync.RWMutex))
	return lok.(*sync.RWMutex)
}

var smuxConf = &smux.Config{
	Version:           2,
	KeepAliveInterval: time.Minute * 1,
	KeepAliveTimeout:  time.Minute * 2,
	MaxFrameSize:      32768,
	MaxReceiveBuffer:  100 * 1024 * 1024,
	MaxStreamBuffer:   100 * 1024 * 1024,
}

// Dial dials a "pseudoTCP" connection to the given host
func Dial(host string) (conn net.Conn, err error) {
	getLock(host).Lock()
	defer getLock(host).Unlock()
	fixConn := func() {
		conn.SetDeadline(time.Now().Add(time.Second * 10))
		buf := make([]byte, 1)
		conn.Write(buf)
		io.ReadFull(conn, buf)
		conn.SetDeadline(time.Time{})
	}
	if s, ok := connPool.smuxes.Load(host); ok {
		ssess := s.(*smux.Session)
		conn, err = ssess.OpenStream()
		if err != nil {
			connPool.smuxes.Delete(host)
		} else {
			fixConn()
		}
		return
	}

	rawConn, err := net.Dial("tcp", host)
	if err != nil {
		return
	}
	ssess, err := smux.Client(rawConn, smuxConf)
	if err != nil {
		rawConn.Close()
		return
	}
	connPool.smuxes.Store(host, ssess)
	conn, err = ssess.OpenStream()
	if err == nil {
		fixConn()
	}
	return
}

// Listener listens for ptcp connections
type Listener struct {
	death      tomb.Tomb
	incoming   chan net.Conn
	underlying net.Listener
}

// Listen opens a Listener
func Listen(addr string) (listener net.Listener, err error) {
	tListener, err := net.Listen("tcp", addr)
	if err != nil {
		return
	}
	toret := &Listener{incoming: make(chan net.Conn), underlying: tListener}
	go func() {
		defer toret.death.Kill(io.ErrClosedPipe)
		for {
			rawConn, err := tListener.Accept()
			if err != nil {
				log.Println("raw accept:", err)
				break
			}
			go func() {
				defer rawConn.Close()
				srv, err := smux.Server(rawConn, smuxConf)
				if err != nil {
					log.Println("smux create:", err)
					return
				}
				for {
					conn, err := srv.AcceptStream()
					if err != nil {
						log.Println("smux accept:", err)
						return
					}
					go func() {
						conn.SetDeadline(time.Now().Add(time.Second * 10))
						buf := make([]byte, 1)
						io.ReadFull(conn, buf)
						conn.Write(buf)
						conn.SetDeadline(time.Time{})
						select {
						case toret.incoming <- conn:
						case <-toret.death.Dying():
							srv.Close()
							tListener.Close()
							return
						}
					}()
				}
			}()
		}
	}()
	listener = toret
	return
}

// Accept accepts a new connection.
func (l *Listener) Accept() (conn net.Conn, err error) {
	select {
	case conn = <-l.incoming:
	case <-l.death.Dying():
		err = l.death.Err()
	}
	return
}

// Addr is the address of the listener.
func (l *Listener) Addr() net.Addr {
	return l.underlying.Addr()
}

// Close closes the listener.
func (l *Listener) Close() error {
	l.death.Kill(io.ErrClosedPipe)
	return nil
}
