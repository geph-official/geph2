package pseudotcp

import (
	"io"
	"log"
	"math/rand"
	"net"
	"sync"
	"time"

	"github.com/geph-official/geph2/libs/buffconn"
	"github.com/xtaci/smux"
	"gopkg.in/tomb.v1"
)

var dialArray = make([]*dialer, 32)

func init() {
	for i := range dialArray {
		dialArray[i] = new(dialer)
	}
}

// Dial haha
func Dial(host string) (conn net.Conn, err error) {
	return dialArray[rand.Int()%len(dialArray)].Dial(host)
}

type dialer struct {
	locks  sync.Map // string => *sync.RWMutex
	smuxes sync.Map // string => *smux.Session
}

func (dl *dialer) getLock(host string) *sync.RWMutex {
	lok, _ := dl.locks.LoadOrStore(host, new(sync.RWMutex))
	return lok.(*sync.RWMutex)
}

var smuxConf = &smux.Config{
	Version:           2,
	KeepAliveInterval: time.Minute * 1,
	KeepAliveTimeout:  time.Minute * 2,
	MaxFrameSize:      32768,
	MaxReceiveBuffer:  10 * 1024 * 1024,
	MaxStreamBuffer:   10 * 1024 * 1024,
}

// Dial dials a "pseudoTCP" connection to the given host
func (dl *dialer) Dial(host string) (conn net.Conn, err error) {
	dl.getLock(host).Lock()
	defer dl.getLock(host).Unlock()
	fixConn := func() {
		conn.SetDeadline(time.Now().Add(time.Second * 10))
		buf := make([]byte, 1)
		conn.Write(buf)
		io.ReadFull(conn, buf)
		conn.SetDeadline(time.Time{})
	}
	if s, ok := dl.smuxes.Load(host); ok {
		ssess := s.(*smux.Session)
		conn, err = ssess.OpenStream()
		if err != nil {
			dl.smuxes.Delete(host)
		} else {
			fixConn()
		}
		return
	}

	rawConn, err := net.Dial("tcp", host)
	if err != nil {
		return
	}
	ssess, err := smux.Client(buffconn.New(rawConn), smuxConf)
	if err != nil {
		rawConn.Close()
		return
	}
	dl.smuxes.Store(host, ssess)
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
				srv, err := smux.Server(buffconn.New(rawConn), smuxConf)
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
