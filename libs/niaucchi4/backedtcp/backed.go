package backedtcp

import (
	"encoding/binary"
	"io"
	"log"
	"net"
	"sync"
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
	bw     backedWriter
	remsn  uint64
	wire   net.Conn
	wready chan bool
	glock  sync.RWMutex
	dead   bool
}

func NewSocket(wire net.Conn) *Socket {
	s := &Socket{
		wire:   wire,
		wready: make(chan bool),
	}
	close(s.wready)
	return s
}

func (sock *Socket) Replace(conn net.Conn) (err error) {
	// write-lock here. prevent readers and writers from intruding
	sock.glock.RLock()
	sock.wire.Close()
	sock.glock.RUnlock()
	sock.glock.Lock()
	defer sock.glock.Unlock()
	if sock.dead {
		err = io.ErrClosedPipe
		return
	}
	sock.wire.Close()
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
	log.Println("got their last seen", sock.remsn)
	sock.wire = conn
	toFix := sock.bw.since(theirLastSeen)
	sock.wready = make(chan bool)
	go func() {
		if toFix == nil {
			log.Println("DEATH!")
		} else {
			log.Println("writing missing", len(toFix))
			conn.Write(toFix)
			close(sock.wready)
		}
	}()
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

func (sock *Socket) Read(p []byte) (n int, err error) {
	for i := 1; i < 100; i++ {
		sock.glock.RLock()
		if sock.dead {
			sock.wire.Close()
			err = io.ErrClosedPipe
			return
		}
		wire := sock.wire
		// read lock to make sure stuff doesn't get replaced suddenly
		n, err = wire.Read(p)
		if err == nil {
			sock.remsn += uint64(n)
			sock.glock.RUnlock()
			return
		}
		sock.glock.RUnlock()
		sock.wire.Close()
		log.Println("read error is", err)
		time.Sleep(time.Second * time.Duration(i))
	}
	return
}

func (sock *Socket) Write(p []byte) (n int, err error) {
	for i := 1; i < 100; i++ {
		// read lock to make sure stuff doesn't get replaced suddenly
		sock.glock.RLock()
		if sock.dead {
			sock.wire.Close()
			err = io.ErrClosedPipe
			return
		}
		<-sock.wready
		wire := sock.wire
		sock.glock.RUnlock()
		n, err = wire.Write(p)
		if err == nil {
			sock.bw.addData(p[:n])
			return
		}
		wire.Close()
		log.Println("write error is", err)
		time.Sleep(time.Second * time.Duration(i))
	}
	return
}

func (sock *Socket) LocalAddr() net.Addr {
	return dummyAddr("dummy-local")
}

func (sock *Socket) RemoteAddr() net.Addr {
	return dummyAddr("dummy-remote")
}

func (sock *Socket) SetDeadline(time.Time) error {
	return nil
}

func (sock *Socket) SetReadDeadline(time.Time) error {
	return nil
}

func (sock *Socket) SetWriteDeadline(time.Time) error {
	return nil
}
