package niaucchi3

import (
	"encoding/binary"
	"fmt"
	"log"
	mrand "math/rand"
	"net"
	"time"

	"github.com/patrickmn/go-cache"
)

type sessAddr uint64

func (sa sessAddr) Network() string {
	return "e2e-sess"
}

func (sa sessAddr) String() string {
	return fmt.Sprintf("sid-%v", uint64(sa))
}

// E2ESocket is a niaucchi3 end-to-end socket.
type E2ESocket struct {
	h2ss  *cache.Cache // maps hosts to sids for hosts identified by h:p we contact first
	ss2h  *cache.Cache // maps sids back to hosts for hosts identified by sid that contact us first
	wire  net.PacketConn
	rdbuf [65536]byte
}

func (ee *E2ESocket) ReadFrom(p []byte) (n int, addr net.Addr, err error) {
RESTART:
	wn, waddr, err := ee.wire.ReadFrom(ee.rdbuf[:])
	if err != nil {
		return
	}
	if wn < 8 {
		log.Println("undecodable e2e")
		goto RESTART
	}
	bts := ee.rdbuf[:wn]
	// session ID
	sid := binary.BigEndian.Uint64(bts[:8])
	n = copy(p, bts[8:])
	if _, ok := ee.h2ss.Get(waddr.String()); ok {
		addr = waddr
	} else {
		addr = sessAddr(sid)
	}
	ee.ss2h.SetDefault(fmt.Sprint(sid), waddr)
	return
}

func (ee *E2ESocket) WriteTo(b []byte, addr net.Addr) (int, error) {
	//log.Println("WriteTo", len(b))
	var sid uint64
	var remAddr net.Addr
	switch addr.(type) {
	case sessAddr:
		sid = uint64(addr.(sessAddr))
		if rai, ok := ee.ss2h.Get(fmt.Sprint(sid)); ok {
			remAddr = rai.(net.Addr)
		} else {
			log.Println("e2e write to unknown sessid", sid)
			return 0, nil
		}
	default:
		remAddr = addr
		if sidi, ok := ee.h2ss.Get(addr.String()); ok {
			sid = sidi.(uint64)
		} else { // otherwise we assign an arbitrary sid
			sid = mrand.Uint64()
			ee.h2ss.SetDefault(addr.String(), sid)
		}
	}
	sidbts := make([]byte, 8)
	binary.BigEndian.PutUint64(sidbts, sid)
	toWrite := append(sidbts, b...)
	_, err := ee.wire.WriteTo(toWrite, remAddr)
	if err != nil {
		return 0, err
	}
	return len(b), nil
}

// E2EListen creates a new niaucchi3 end-to-end socket.
func E2EListen(wire net.PacketConn) *E2ESocket {
	return &E2ESocket{
		h2ss: cache.New(time.Hour, time.Hour),
		ss2h: cache.New(time.Hour, time.Hour),
		wire: wire,
	}
}

func (ee *E2ESocket) Close() error {
	return ee.wire.Close()
}

func (ee *E2ESocket) LocalAddr() net.Addr {
	return ee.wire.LocalAddr()
}

func (ee *E2ESocket) SetDeadline(t time.Time) error {
	return ee.wire.SetDeadline(t)
}

func (ee *E2ESocket) SetReadDeadline(t time.Time) error {
	return ee.wire.SetReadDeadline(t)
}
func (ee *E2ESocket) SetWriteDeadline(t time.Time) error {
	return ee.wire.SetWriteDeadline(t)
}
