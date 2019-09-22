package niaucchi4

import (
	"log"
	"net"
	"sync"
	"time"

	"github.com/patrickmn/go-cache"
)

// ObfsSocket represents an obfuscated PacketConn.
type ObfsSocket struct {
	cookie  []byte
	sscache *cache.Cache
	tunnels *cache.Cache
	pending *cache.Cache
	wire    net.PacketConn
	wlock   sync.Mutex
	rdbuf   [65536]byte
}

// ObfsListen opens a new obfuscated PacketConn.
func ObfsListen(cookie []byte, wire net.PacketConn) *ObfsSocket {
	return &ObfsSocket{
		cookie:  cookie,
		sscache: cache.New(time.Minute, time.Hour),
		tunnels: cache.New(time.Hour, time.Hour),
		pending: cache.New(time.Minute, time.Hour),
		wire:    wire,
	}
}

func (os *ObfsSocket) WriteTo(b []byte, addr net.Addr) (int, error) {
	if tuni, ok := os.tunnels.Get(addr.String()); ok {
		tun := tuni.(*tunstate)
		toWrite := tun.Encrypt(b)
		_, err := os.wire.WriteTo(toWrite, addr)
		if err != nil {
			return 0, err
		}
		return len(b), nil
	}
	// if we are pending, just ignore
	if _, ok := os.pending.Get(addr.String()); ok {
		log.Println("pretending to write since ALREADY PENDING")
		return len(b), nil
	}
	// establish a conn
	pt, hello := newproto(os.cookie)
	log.Println("pretending to write, actually ESTABLISHING")
	os.pending.SetDefault(addr.String(), pt)
	os.wlock.Lock()
	_, err := os.wire.WriteTo(hello, addr)
	os.wlock.Unlock()
	if err != nil {
		return 0, err
	}
	return len(b), nil
}

func (os *ObfsSocket) ReadFrom(p []byte) (n int, addr net.Addr, err error) {
RESTART:
	readBytes, addr, err := os.wire.ReadFrom(os.rdbuf[:])
	if err != nil {
		return
	}
	// check if the packet belongs to a known tunnel
	if tuni, ok := os.tunnels.Get(addr.String()); ok {
		tun := tuni.(*tunstate)
		//log.Println("got packet of known tunnel from", addr.String())
		plain, e := tun.Decrypt(os.rdbuf[:readBytes])
		if e != nil {
			log.Println("got undecryptable at", addr, e)
			goto RESTART
		}
		n = copy(p, plain)
		return
	}
	// check if the packet belongs to a pending thing
	if proti, ok := os.pending.Get(addr.String()); ok {
		prot := proti.(*prototun)
		ts, e := prot.realize(os.rdbuf[:readBytes], false)
		if e != nil {
			log.Println("got bad response to pending", addr, e)
			goto RESTART
		}
		os.tunnels.SetDefault(addr.String(), ts)
		log.Println("got realization of pending", addr)
		goto RESTART
	}
	// otherwise it has to be some sort of tunnel opener
	log.Println("got suspected hello")
	pt, myhello := newproto(os.cookie)
	ts, e := pt.realize(os.rdbuf[:readBytes], true)
	if e != nil {
		log.Println("got bad hello", e)
		goto RESTART
	}
	os.wlock.Lock()
	os.wire.WriteTo(myhello, addr)
	os.wlock.Unlock()
	os.tunnels.SetDefault(addr.String(), ts)
	log.Println("got opened tunnel at", addr)
	goto RESTART
}

func (os *ObfsSocket) Close() error {
	return os.wire.Close()
}

func (os *ObfsSocket) LocalAddr() net.Addr {
	return os.wire.LocalAddr()
}

func (os *ObfsSocket) SetDeadline(t time.Time) error {
	return os.wire.SetDeadline(t)
}

func (os *ObfsSocket) SetReadDeadline(t time.Time) error {
	return os.wire.SetReadDeadline(t)
}
func (os *ObfsSocket) SetWriteDeadline(t time.Time) error {
	return os.wire.SetWriteDeadline(t)
}
