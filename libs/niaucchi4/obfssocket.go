package niaucchi4

import (
	"fmt"
	"log"
	"net"
	"os"
	"sync"
	"time"

	"github.com/hashicorp/golang-lru/simplelru"
	"github.com/patrickmn/go-cache"
)

var doLogging = false

func init() {
	doLogging = os.Getenv("N4LOG") != ""
}

type oAddr []byte

func (sa oAddr) Network() string {
	return "o-sess"
}

func (sa oAddr) String() string {
	return fmt.Sprintf("osid-%x", []byte(sa)[:10])
}

// ObfsSocket represents an obfuscated PacketConn.
type ObfsSocket struct {
	cookie           []byte
	cookieExceptions sync.Map
	sscache          *simplelru.LRU
	tunnels          *simplelru.LRU
	pending          *simplelru.LRU
	wire             net.PacketConn
	wlock            sync.Mutex
	rdbuf            [65536]byte
}

func newLRU() *simplelru.LRU {
	l, e := simplelru.NewLRU(8192, nil)
	if e != nil {
		panic(e)
	}
	return l
}

// ObfsListen opens a new obfuscated PacketConn.
func ObfsListen(cookie []byte, wire net.PacketConn) *ObfsSocket {
	return &ObfsSocket{
		cookie:  cookie,
		sscache: newLRU(),
		tunnels: newLRU(),
		pending: newLRU(),
		wire:    wire,
	}
}

// AddCookieException adds a cookie exception to a particular destination.
func (os *ObfsSocket) AddCookieException(addr net.Addr, cookie []byte) {
	os.cookieExceptions.Store(addr, cookie)
}

func (os *ObfsSocket) WriteTo(b []byte, addr net.Addr) (int, error) {
	os.wlock.Lock()
	defer os.wlock.Unlock()
	var isHidden bool
	switch addr.(type) {
	case oAddr:
		v, ok := os.sscache.Get(string(addr.(oAddr)))
		if !ok {
			return 0, nil
		}
		addr = v.(net.Addr)
		isHidden = true
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
	if isHidden {
		return 0, nil
	}
	// if we are pending, just ignore
	if zz, ok := os.pending.Get(addr.String()); ok {
		os.wire.WriteTo(zz.(*prototun).genHello(), addr)
		if doLogging {
			log.Println("N4: retransmitting existing pending to", addr.String())
		}
		return len(b), nil
	}
	// establish a conn
	cookie := os.cookie
	if v, ok := os.cookieExceptions.Load(addr); ok {
		cookie = v.([]byte)
	}
	pt := newproto(cookie)
	if doLogging {
		log.Printf("N4: newproto on %v [%x]", addr.String(), cookie)
	}
	os.pending.Add(addr.String(), pt)
	var err error
	_, err = os.wire.WriteTo(pt.genHello(), addr)
	if err != nil {
		return 0, nil
	}
	return len(b), nil
}

func (os *ObfsSocket) ReadFrom(b []byte) (n int, addr net.Addr, err error) {
	for n == 0 && err == nil {
		n, addr, err = os.hiddenReadFrom(b)
	}
	return
}

var badHelloBlacklist = cache.New(time.Minute*5, time.Minute)

func (os *ObfsSocket) hiddenReadFrom(p []byte) (n int, addr net.Addr, err error) {
	readBytes, addr, err := os.wire.ReadFrom(os.rdbuf[:])
	if err != nil {
		return
	}
	if _, ok := badHelloBlacklist.Get(addr.String()); ok {
		return
	}
	os.wlock.Lock()
	defer os.wlock.Unlock()
	// check if the packet belongs to a known tunnel
	if tuni, ok := os.tunnels.Get(addr.String()); ok {
		tun := tuni.(*tunstate)
		plain, e := tun.Decrypt(os.rdbuf[:readBytes])
		if e == nil {
			os.tunnels.Add(addr.String(), tun)
			n = copy(p, plain)
			if _, ok := os.sscache.Get(string(tun.ss)); ok {
				os.sscache.Add(string(tun.ss), addr)
				addr = oAddr(tun.ss)
			}
			return
		}
	}
	// check if the packet belongs to a pending thing
	if proti, ok := os.pending.Get(addr.String()); ok {
		prot := proti.(*prototun)
		ts, e := prot.realize(os.rdbuf[:readBytes], false)
		if e != nil {
			return
		}
		os.tunnels.Add(addr.String(), ts)
		if doLogging {
			log.Println("N4: got realization of pending", addr)
		}
		return
	}
	// iterate through all the stuff to create an association
	for _, k := range os.tunnels.Keys() {
		v, ok := os.tunnels.Get(k)
		if !ok {
			continue
		}
		tun := v.(*tunstate)
		plain, e := tun.Decrypt(os.rdbuf[:readBytes])
		if e == nil {
			os.tunnels.Remove(k)
			if doLogging {
				log.Println("N4: found a decryptable session through scanning, ROAM to", addr)
			}
			os.tunnels.Add(addr.String(), tun)
			n = copy(p, plain)
			if _, ok := os.sscache.Get(string(tun.ss)); ok {
				os.sscache.Add(string(tun.ss), addr)
				addr = oAddr(tun.ss)
			}
			return
		}
	}
	// otherwise it has to be some sort of tunnel opener
	//log.Println("got suspected hello")
	pt := newproto(os.cookie)
	ts, e := pt.realize(os.rdbuf[:readBytes], true)
	if e != nil {
		if doLogging {
			log.Println("N4: bad hello from", addr.String(), e.Error())
		}
		badHelloBlacklist.SetDefault(addr.String(), true)
		return
	}
	os.tunnels.Add(addr.String(), ts)
	os.sscache.Add(string(ts.ss), addr)
	go func() {
		os.wlock.Lock()
		defer os.wlock.Unlock()
		os.wire.WriteTo(pt.genHello(), addr)
		if doLogging {
			log.Println("N4: responded to a hello from", addr)
		}
	}()
	return
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
