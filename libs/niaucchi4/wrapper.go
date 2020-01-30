package niaucchi4

import (
	"io"
	"log"
	"math/rand"
	"net"
	"sync"
	"time"

	"gopkg.in/tomb.v1"
)

type wrapperRead struct {
	bts    []byte
	rmAddr net.Addr
}

// Wrapper is a PacketConn that can be hot-replaced by other PacketConns on I/O failure or manually. It squelches any errors bubbling up.
type Wrapper struct {
	wireMap    map[net.Addr]net.PacketConn
	getConn    func() net.PacketConn
	nextExpire time.Time
	wmlock     sync.Mutex
	incoming   chan wrapperRead
	death      tomb.Tomb
}

// Wrap creates a new Wrapper instance.
func Wrap(getConn func() net.PacketConn) *Wrapper {
	return &Wrapper{
		wireMap:  make(map[net.Addr]net.PacketConn),
		getConn:  getConn,
		incoming: make(chan wrapperRead, 128),
	}
}

func (w *Wrapper) getWire(addr net.Addr) net.PacketConn {
	w.wmlock.Lock()
	defer w.wmlock.Unlock()
retry:
	wire, ok := w.wireMap[addr]
	if !ok {
		newWire := w.getConn()
		w.wireMap[addr] = newWire
		go func() {
			// delete on exit
			defer func() {
				w.wmlock.Lock()
				if w.wireMap[addr] != newWire {
					panic("WHAT")
				}
				delete(w.wireMap, addr)
				w.wmlock.Unlock()
			}()
			buf := malloc(2048)
			for {
				n, a, err := newWire.ReadFrom(buf)
				if err != nil {
					return
				}
				newbuf := malloc(n)
				copy(newbuf, buf)
				select {
				case w.incoming <- wrapperRead{newbuf, a}:
				case <-w.death.Dying():
					return
				}
			}
		}()
		goto retry
	}
	return wire
}

func (w *Wrapper) ReadFrom(p []byte) (n int, addr net.Addr, err error) {
	select {
	case wrapped := <-w.incoming:
		n = copy(p, wrapped.bts)
		addr = wrapped.rmAddr
		free(wrapped.bts)
	case <-w.death.Dying():
		err = w.death.Err()
	}
	return
}

func (w *Wrapper) WriteTo(b []byte, addr net.Addr) (int, error) {
	wire := w.getWire(addr)
	wire.WriteTo(b, addr)
	now := time.Now()
	if now.After(w.nextExpire) {
		oldNextExpire := w.nextExpire
		w.nextExpire = now.Add(time.Second*10 + time.Millisecond*time.Duration(rand.ExpFloat64()*10000))
		zeroTime := time.Time{}
		if oldNextExpire != zeroTime {
			wire.Close()
			if doLogging {
				log.Println("N4: wrapper killing", wire.LocalAddr())
			}
		}
	}
	return len(b), nil
}

func (w *Wrapper) Close() error {
	w.wmlock.Lock()
	defer w.wmlock.Unlock()
	w.death.Kill(io.ErrClosedPipe)
	for _, conn := range w.wireMap {
		conn.Close()
	}
	return nil
}

func (w *Wrapper) LocalAddr() net.Addr {
	w.wmlock.Lock()
	defer w.wmlock.Unlock()
	return nil
}

func (w *Wrapper) SetDeadline(t time.Time) error {
	w.wmlock.Lock()
	defer w.wmlock.Unlock()
	return nil
}

func (w *Wrapper) SetReadDeadline(t time.Time) error {
	w.wmlock.Lock()
	defer w.wmlock.Unlock()
	return nil
}
func (w *Wrapper) SetWriteDeadline(t time.Time) error {
	w.wmlock.Lock()
	defer w.wmlock.Unlock()
	return nil
}

var bufPool = &sync.Pool{
	New: func() interface{} {
		return make([]byte, 2048)
	},
}

func malloc(n int) []byte {
	return bufPool.Get().([]byte)[:n]
}

func free(bts []byte) {
	bufPool.Put(bts[:2048])
}
