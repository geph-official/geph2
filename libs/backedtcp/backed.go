package backedtcp

import (
	"encoding/binary"
	"errors"
	"io"
	"log"
	"net"
	"sync"
	"sync/atomic"
	"time"
)

const maxBufferSize = 1000 * 1000 * 10

type backedWriter struct {
	lastsn uint64
	buffer []byte
	lk     sync.Mutex
}

func (br *backedWriter) addData(ob []byte) {
	b := make([]byte, len(ob))
	copy(b, ob)
	br.lastsn += uint64(len(b))
	br.buffer = append(br.buffer, b...)
	if len(br.buffer) >= maxBufferSize {
		br.buffer = br.buffer[len(br.buffer)-maxBufferSize:]
	}
}

func (br *backedWriter) since(sn uint64) []byte {
	log.Println(br.lastsn, "since", sn)
	if sn == br.lastsn {
		return make([]byte, 0)
	}
	if sn > br.lastsn || sn < br.lastsn-uint64(len(br.buffer)) {
		return nil
	}
	return br.buffer[len(br.buffer)-int(br.lastsn-sn):]
}

type Socket struct {
	bw      backedWriter
	remsn   uint64
	wire    net.Conn
	wready  chan bool
	getWire func() (net.Conn, error)
	glock   sync.RWMutex
	dead    bool

	replaceLock sync.Mutex

	wDeadline atomic.Value
	rDeadline atomic.Value
}

func NewSocket(getWire func() (net.Conn, error)) *Socket {
	s := &Socket{
		getWire: getWire,
		wready:  make(chan bool),
	}
	s.SetDeadline(time.Time{})
	//go s.AutoReplace()
	close(s.wready)
	return s
}

func (sock *Socket) Reset() {
	sock.glock.RLock()
	if sock.wire != nil {
		sock.wire.Close()
	}
	sock.glock.RUnlock()
}

func (sock *Socket) AutoReplace() (err error) {
	sock.replaceLock.Lock()
	defer sock.replaceLock.Unlock()
	conn, err := sock.getWire()
	if err != nil {
		return
	}
	sock.Replace(conn)
	return
}

func (sock *Socket) Replace(conn net.Conn) (err error) {
	// write-lock here. prevent readers and writers from intruding
	sock.glock.RLock()
	if sock.wire != nil {
		sock.wire.Close()
	}
	sock.glock.RUnlock()
	sock.glock.Lock()
	defer sock.glock.Unlock()
	if sock.dead {
		err = io.ErrClosedPipe
		return
	}
	if sock.wire != nil {
		sock.wire.Close()
	}
	// negotiate new stuff
	dun := make(chan bool)
	go func() {
		binary.Write(conn, binary.BigEndian, sock.remsn)
		log.Println("writing our last seen", sock.remsn)
		close(dun)
	}()
	var theirLastSeen uint64
	binary.Read(conn, binary.BigEndian, &theirLastSeen)
	<-dun
	log.Println("got their last seen", theirLastSeen)
	sock.wire = conn
	toFix := sock.bw.since(theirLastSeen)
	if toFix == nil {
		log.Println("their last seen is too far back, B A D!")
		sock.dead = true
		err = errors.New("synchronization error")
		return
	}
	ch := make(chan bool)
	sock.wready = ch
	go func() {
		if toFix == nil {
			log.Println("DEATH!")
		} else {
			log.Println("writing missing", len(toFix))
			conn.Write(toFix)
			close(ch)
		}
	}()
	<-sock.wready
	return nil
}

func (sock *Socket) Close() (err error) {
	sock.glock.RLock()
	sock.wire.Close()
	sock.glock.RUnlock()
	sock.glock.Lock()
	sock.dead = true
	sock.glock.Unlock()
	return
}

var zerotime time.Time

func (sock *Socket) Read(p []byte) (n int, err error) {
	for {
		var rd time.Time
		rd = sock.rDeadline.Load().(time.Time)
		if rd != zerotime && time.Now().After(rd) {
			err = errors.New("big timeout")
			return
		}
		sock.glock.RLock()
		if sock.dead {
			sock.wire.Close()
			err = io.ErrClosedPipe
			return
		}
		if sock.wire != nil {
			wire := sock.wire
			wire.SetDeadline(rd)
			// read lock to make sure stuff doesn't get replaced suddenly
			n, err = wire.Read(p)
			if err == nil {
				sock.remsn += uint64(n)
				sock.glock.RUnlock()
				return
			}
			sock.wire.Close()
		}
		sock.glock.RUnlock()
		log.Println("read error is", err)
		sock.AutoReplace()
		time.Sleep(time.Second)
	}
}

func (sock *Socket) Write(p []byte) (n int, err error) {
	for i := 0; i < 120; i++ {
		var wd time.Time
		wd = sock.wDeadline.Load().(time.Time)
		if wd != zerotime && time.Now().After(wd) {
			err = errors.New("big timeout")
			return
		}
		// read lock to make sure stuff doesn't get replaced suddenly
		sock.glock.RLock()
		if sock.dead {
			sock.wire.Close()
			err = io.ErrClosedPipe
			return
		}
		<-sock.wready
		wire := sock.wire
		if wire != nil {
			wire.SetWriteDeadline(wd)
		}
		sock.glock.RUnlock()
		if wire != nil {
			n, err = wire.Write(p)
			if err == nil {
				sock.bw.addData(p[:n])
				return
			}
			wire.Close()
		}
		log.Println("write error is", err, i)
		//sock.AutoReplace()
		time.Sleep(time.Second)
	}
	err = errors.New("timeout")
	return
}

func (sock *Socket) LocalAddr() net.Addr {
	return dummyAddr("dummy-local")
}

func (sock *Socket) RemoteAddr() net.Addr {
	return dummyAddr("dummy-remote")
}

func (sock *Socket) SetDeadline(t time.Time) error {
	sock.SetReadDeadline(t)
	sock.SetWriteDeadline(t)
	return nil
}

func (sock *Socket) SetReadDeadline(t time.Time) error {
	sock.rDeadline.Store(t)
	return nil
}

func (sock *Socket) SetWriteDeadline(t time.Time) error {
	sock.wDeadline.Store(t)
	return nil
}

type dummyAddr string

func (da dummyAddr) String() string {
	return string(da)
}

func (da dummyAddr) Network() string {
	return string(da)
}
