package niaucchi4

import (
	"fmt"
	"log"
	"net"
	"sync"
	"time"

	"github.com/patrickmn/go-cache"
)

type oAddr []byte

func (sa oAddr) Network() string {
	return "o-sess"
}

func (sa oAddr) String() string {
	return fmt.Sprintf("osid-%x", []byte(sa)[:10])
}

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
		sscache: cache.New(time.Hour, time.Hour),
		tunnels: cache.New(time.Hour, time.Hour),
		pending: cache.New(time.Second, time.Hour),
		wire:    wire,
	}
}

func (os *ObfsSocket) WriteTo(b []byte, addr net.Addr) (int, error) {
	switch addr.(type) {
	case oAddr:
		v, ok := os.sscache.Get(string(addr.(oAddr)))
		if !ok {
			return 0, nil
		}
		addr = v.(net.Addr)
	}
	if tuni, ok := os.tunnels.Get(addr.String()); ok {
		tun := tuni.(*tunstate)
		toWrite := tun.Encrypt(b)
		_, err := os.wire.WriteTo(toWrite, addr)
		if err != nil {
			return 0, nil
		}
		return len(b), nil
	}
	// if we are pending, just ignore
	if zz, ok := os.pending.Get(addr.String()); ok {
		//log.Println("pretending to write since ALREADY PENDING")
		os.wlock.Lock()
		os.wire.WriteTo(zz.(*prototun).hello, addr)
		os.wlock.Unlock()
		return len(b), nil
	}
	// establish a conn
	pt, hello := newproto(os.cookie)
	log.Println("N4: establishing to", addr.String())
	//log.Println("pretending to write, actually ESTABLISHING")
	os.pending.SetDefault(addr.String(), pt)
	os.wlock.Lock()
	_, err := os.wire.WriteTo(hello, addr)
	os.wlock.Unlock()
	if err != nil {
		return 0, nil
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
			goto RESTART
		}
		os.tunnels.SetDefault(addr.String(), tun)
		n = copy(p, plain)
		if _, ok := os.sscache.Get(string(tun.ss)); ok {
			os.sscache.SetDefault(string(tun.ss), addr)
			addr = oAddr(tun.ss)
		}
		return
	}
	// check if the packet belongs to a pending thing
	if proti, ok := os.pending.Get(addr.String()); ok {
		prot := proti.(*prototun)
		ts, e := prot.realize(os.rdbuf[:readBytes], false)
		if e != nil {
			//log.Println("got bad response to pending", addr, e)
			goto RESTART
		}
		os.tunnels.SetDefault(addr.String(), ts)
		log.Println("got realization of pending", addr)
		goto RESTART
	}
	// iterate through all the stuff to create an association
	for k, v := range os.tunnels.Items() {
		if v.Expired() {
			continue
		}
		tun := v.Object.(*tunstate)
		plain, e := tun.Decrypt(os.rdbuf[:readBytes])
		if e == nil {
			os.tunnels.Delete(k)
			log.Println("found a decryptable session through scanning, ROAM to", addr)
			os.tunnels.SetDefault(addr.String(), tun)
			n = copy(p, plain)
			if _, ok := os.sscache.Get(string(tun.ss)); ok {
				os.sscache.SetDefault(string(tun.ss), addr)
				addr = oAddr(tun.ss)
			}
			return
		}
	}
	// otherwise it has to be some sort of tunnel opener
	//log.Println("got suspected hello")
	pt, myhello := newproto(os.cookie)
	ts, e := pt.realize(os.rdbuf[:readBytes], true)
	if e != nil {
		// log.Println("got bad hello", e)
		goto RESTART
	}
	os.wlock.Lock()
	os.wire.WriteTo(myhello, addr)
	log.Println("responded to a hello from", addr)
	os.wlock.Unlock()
	os.tunnels.SetDefault(addr.String(), ts)
	os.sscache.SetDefault(string(ts.ss), addr)
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
