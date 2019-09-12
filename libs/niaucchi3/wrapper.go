package niaucchi3

import (
	"errors"
	"log"
	"net"
	"sync"
	"time"
)

// Wrapper is a PacketConn that can be hot-replaced by other PacketConns on I/O failure or manually. It squelches any errors bubbling up.
type Wrapper struct {
	wire    net.PacketConn
	getConn func() net.PacketConn
	lock    sync.Mutex
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
		n, addr, err = wire.ReadFrom(p)
	}
	if err != nil {
		log.Println("wrapper squelching", err)
		w.lock.Lock()
		w.wire = w.getConn()
		w.lock.Unlock()
		goto retry
	}
	return
}

func (w *Wrapper) WriteTo(b []byte, addr net.Addr) (int, error) {
	w.lock.Lock()
	defer w.lock.Unlock()
	if w.wire != nil {
		w.wire.WriteTo(b, addr)
	}
	return len(b), nil
}

func (w *Wrapper) Close() error {
	w.lock.Lock()
	defer w.lock.Unlock()
	if w.wire != nil {
		w.wire.Close()
	}
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
