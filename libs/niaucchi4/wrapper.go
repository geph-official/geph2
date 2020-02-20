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
	wireMap   map[net.Addr]net.PacketConn
	expireMap map[net.Addr]time.Time
	getConn   func() net.PacketConn
	wmlock    sync.Mutex
	incoming  chan wrapperRead
	death     tomb.Tomb
}

// Wrap creates a new Wrapper instance.
func Wrap(getConn func() net.PacketConn) *Wrapper {
	return &Wrapper{
		wireMap:   make(map[net.Addr]net.PacketConn),
		expireMap: make(map[net.Addr]time.Time),
		getConn:   getConn,
		incoming:  make(chan wrapperRead, 128),
	}
}

func (w *Wrapper) getExpire(addr net.Addr) time.Time {
	w.wmlock.Lock()
	defer w.wmlock.Unlock()
	return w.expireMap[addr]
}

func (w *Wrapper) setExpire(addr net.Addr, t time.Time) {
	w.wmlock.Lock()
	defer w.wmlock.Unlock()
	w.expireMap[addr] = t
}

func (w *Wrapper) getWire(addr net.Addr) net.PacketConn {
	w.wmlock.Lock()
	defer w.wmlock.Unlock()
retry:
	wire, ok := w.wireMap[addr]
	if !ok {
		newWire := w.getConn()
		incrOpenWires()
		w.wireMap[addr] = newWire
		if doLogging {
			log.Println("N4: set", addr, "=>", newWire.LocalAddr())
		}
		go func() {
			defer newWire.Close()
			// delete on exit
			defer func() {
				w.wmlock.Lock()
				if w.wireMap[addr] != newWire {
					if doLogging {
						log.Println("N4: wire already replaced, don't delete")
					}
				} else {
					delete(w.wireMap, addr)
				}
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

var openWires int64

func incrOpenWires() {
	//log.Println("openWires => ", atomic.AddInt64(&openWires, 1))
}

func decrOpenWires() {
	//log.Println("openWires => ", atomic.AddInt64(&openWires, -1))
}

func (w *Wrapper) WriteTo(b []byte, addr net.Addr) (int, error) {
	wire := w.getWire(addr)
	wire.WriteTo(b, addr)
	now := time.Now()
	expire := w.getExpire(addr)
	if now.After(expire) {
		w.setExpire(addr, now.Add(time.Second*30+time.Millisecond*time.Duration(rand.ExpFloat64()*30000)))
		zeroTime := time.Time{}
		if expire != zeroTime {
			w.wmlock.Lock()
			if w.wireMap[addr] == wire {
				delete(w.wireMap, addr)
			}
			w.wmlock.Unlock()
			go func() {
				time.Sleep(time.Second * 10)
				decrOpenWires()
				wire.Close()
			}()
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
