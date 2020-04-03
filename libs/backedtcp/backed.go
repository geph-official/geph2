package backedtcp

import (
	"bytes"
	"encoding/binary"
	"errors"
	"io"
	"net"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	pool "github.com/libp2p/go-buffer-pool"
	"gopkg.in/tomb.v1"
)

const maxBufferSize = 1000 * 1000

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
	//log.Println("addData buffer size now", cap(br.buffer))
	if len(br.buffer) >= maxBufferSize {
		br.buffer = br.buffer[len(br.buffer)-maxBufferSize:]
	}
}

func (br *backedWriter) reset() {
	if len(br.buffer) > 100*1000 {
		newlen := len(br.buffer) / 2
		nbuf := make([]byte, newlen)
		copy(nbuf, br.buffer[len(br.buffer)-newlen:])
		br.buffer = nbuf
	}
}

func (br *backedWriter) since(sn uint64) []byte {
	if sn == br.lastsn {
		return make([]byte, 0)
	}
	if sn > br.lastsn || sn < br.lastsn-uint64(len(br.buffer)) {
		return nil
	}
	return br.buffer[len(br.buffer)-int(br.lastsn-sn):]
}

// Socket represents a single BackedTCP connection
type Socket struct {
	bw        backedWriter
	getWire   func() (net.Conn, error)
	chWrite   chan []byte
	chRead    chan []byte
	chReplace chan struct{}
	readBuf   bytes.Buffer
	readBytes uint64
	death     tomb.Tomb
	remAddr   atomic.Value
	locAddr   atomic.Value

	rDeadline atomic.Value
	wDeadline atomic.Value
}

// NewSocket constructs a new BackedTCP connection.
func NewSocket(getWire func() (net.Conn, error)) *Socket {
	s := &Socket{
		getWire:   getWire,
		chWrite:   make(chan []byte),
		chRead:    make(chan []byte),
		chReplace: make(chan struct{}),
	}
	s.SetDeadline(time.Time{})
	go s.mainLoop()
	return s
}

func (sock *Socket) mainLoop() {
	for {
		select {
		case <-sock.death.Dying():
			return
		default:
		}
		// first we get a wire
		wire, err := sock.getWire()
		if err != nil {
			// this is fatal
			sock.death.Kill(err)
			return
		}
		wra := wire.RemoteAddr()
		wla := wire.LocalAddr()
		sock.remAddr.Store(&wra)
		sock.locAddr.Store(&wla)
		stopWrite := make(chan struct{})
		// negotiation shouldn't take more than 10 secs
		wire.SetDeadline(time.Now().Add(time.Second * 10))
		sent := make(chan bool)
		// negotiate
		go func() {
			defer close(sent)
			// we write our total bytes read. in a new goroutine to prevent dedlock
			binary.Write(wire, binary.BigEndian, sock.readBytes)
		}()
		// read the remote bytes read
		var theirReadBytes uint64
		err = binary.Read(wire, binary.BigEndian, &theirReadBytes)
		if err != nil {
			wire.Close()
			continue
		}
		<-sent
		// get the data that needs to be resent
		toResend := sock.bw.since(theirReadBytes)
		if toResend == nil {
			// out of range
			sock.death.Kill(errors.New("out of resumption range"))
			return
		}
		wire.SetDeadline(time.Time{})
		done := make(chan bool)
		go func() {
			defer close(done)
			defer close(stopWrite)
			sock.readLoop(wire)
		}()
		sock.writeLoop(toResend, wire, stopWrite)
		<-done
	}
}

func (sock *Socket) writeLoop(toResend []byte, wire net.Conn, stopWrite chan struct{}) {
	defer wire.Close()
	wire.SetWriteDeadline(sock.wDeadline.Load().(time.Time))
	_, err := wire.Write(toResend)
	if err != nil {
		return
	}
	for {
		var timeout <-chan time.Time
		if sock.bw.buffer != nil {
			timeout = time.After(time.Second * 10)
		}
		select {
		case toWrite := <-sock.chWrite:
			// first we remember this so that we can restore
			sock.bw.addData(toWrite)
			wire.SetWriteDeadline(sock.wDeadline.Load().(time.Time))
			// then we try to write. it's okay if we fail!
			_, err := wire.Write(toWrite)
			pool.GlobalPool.Put(toWrite)
			if err != nil {
				if strings.Contains(err.Error(), "timeout") {
					sock.death.Kill(err)
				}
				return
			}
		case <-timeout:
			sock.bw.reset()
		case <-stopWrite:
			//log.Println("writeLoop stopped")
			return
		case <-sock.chReplace:
			//log.Println("writeLoop stopped for replace")
			return
		case <-sock.death.Dying():
			//log.Println("writeLoop forced to die", sock.death.Err())
			return
		}
	}
}

func (sock *Socket) readLoop(wire net.Conn) {
	defer wire.Close()
	// just loop and read and feed into the channel
	for {
		wire.SetReadDeadline(sock.rDeadline.Load().(time.Time))
		buf := pool.GlobalPool.Get(65536)
		n, err := wire.Read(buf)
		if err != nil {
			return
		}
		sock.readBytes += uint64(n)
		sock.chRead <- buf[:n]
	}
}

// Reset forces the socket to discard its underlying connection and reconnect.
func (sock *Socket) Reset() (err error) {
	select {
	case sock.chReplace <- struct{}{}:
		return
	case <-sock.death.Dying():
		err = sock.death.Err()
		return
	}
}

// Close closes the socket.
func (sock *Socket) Close() (err error) {
	sock.death.Kill(io.ErrClosedPipe)
	return
}

func (sock *Socket) Read(p []byte) (n int, err error) {
	for {
		if sock.readBuf.Len() > 0 {
			return sock.readBuf.Read(p)
		}
		select {
		case <-sock.death.Dying():
			err = sock.death.Err()
			return
		case bts := <-sock.chRead:
			sock.readBuf.Write(bts)
			pool.GlobalPool.Put(bts)
		}
	}
}

func (sock *Socket) Write(p []byte) (n int, err error) {
	buf := pool.GlobalPool.Get(len(p))
	copy(buf, p)
	select {
	case sock.chWrite <- buf:
		n = len(p)
		return
	case <-sock.death.Dying():
		err = sock.death.Err()
		return
	}
}

func (sock *Socket) LocalAddr() net.Addr {
	zz := sock.locAddr.Load()
	if zz == nil {
		return dummyAddr("dummy-local")
	}
	return *(zz.(*net.Addr))
}

func (sock *Socket) RemoteAddr() net.Addr {
	zz := sock.remAddr.Load()
	if zz == nil {
		return dummyAddr("dummy-remote")
	}
	return *(zz.(*net.Addr))
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
