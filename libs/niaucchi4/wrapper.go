package niaucchi4

import (
	"errors"
	"math/rand"
	"net"
	"sync"
	"time"
)

// Wrapper is a PacketConn that can be hot-replaced by other PacketConns on I/O failure or manually. It squelches any errors bubbling up.
type Wrapper struct {
	wire       net.PacketConn
	nextWire   net.PacketConn
	getConn    func() net.PacketConn
	nextExpire time.Time
	lock       sync.Mutex
}

// Wrap creates a new Wrapper instance.
func Wrap(getConn func() net.PacketConn) *Wrapper {
	return &Wrapper{
		getConn: getConn,
	}
}

func (w *Wrapper) ReadFrom(p []byte) (n int, addr net.Addr, err error) {
retry:
	w.lock.Lock()
	wire := w.wire
	w.lock.Unlock()
	if w.wire == nil {
		err = errors.New("nil")
	} else {
		wire.SetReadDeadline(time.Now().Add(time.Minute * 30))
		n, addr, err = wire.ReadFrom(p)
	}
	if err != nil {
		w.lock.Lock()
		if w.wire == wire {
			if w.wire != nil {
				w.wire.Close()
			}
			w.wire = w.getConn()
		}
		w.lock.Unlock()
		goto retry
	}
	return
}

func (w *Wrapper) WriteTo(b []byte, addr net.Addr) (int, error) {
	w.lock.Lock()
	defer w.lock.Unlock()
	wire := w.wire
	if w.nextWire != nil {
		wire = w.nextWire
	}
	if wire != nil {
		if time.Since(w.nextExpire) > 0 && w.nextWire == nil {
			newWire := w.getConn()
			w.nextWire = newWire
			wire = newWire
			go func() {
				zz := make([]byte, 4000)
				newWire.ReadFrom(zz)
				w.lock.Lock()
				defer w.lock.Unlock()
				if w.wire != nil {
					w.wire.Close()
				}
				w.wire = newWire
				w.nextWire = nil
				w.nextExpire = time.Now().Add(time.Millisecond * time.Duration(60000*10+60000*rand.ExpFloat64()))
			}()
		}
		wire.WriteTo(b, addr)
	}
	return len(b), nil
}

func (w *Wrapper) Close() error {
	w.lock.Lock()
	defer w.lock.Unlock()
	if w.wire != nil {
		w.wire.Close()
	}
	w.wire = nil
	return nil
}

func (w *Wrapper) LocalAddr() net.Addr {
	w.lock.Lock()
	defer w.lock.Unlock()
	if w.wire != nil {
		return w.wire.LocalAddr()
	}
	return nil
}

func (w *Wrapper) SetDeadline(t time.Time) error {
	w.lock.Lock()
	defer w.lock.Unlock()
	if w.wire != nil {
		w.wire.SetDeadline(t)
	}
	return nil
}

func (w *Wrapper) SetReadDeadline(t time.Time) error {
	w.lock.Lock()
	defer w.lock.Unlock()
	if w.wire != nil {
		w.wire.SetReadDeadline(t)
	}
	return nil
}
func (w *Wrapper) SetWriteDeadline(t time.Time) error {
	w.lock.Lock()
	defer w.lock.Unlock()
	if w.wire != nil {
		w.wire.SetWriteDeadline(t)
	}
	return nil
}
