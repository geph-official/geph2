package niaucchi4

import (
	"fmt"
	"log"
	"net"
	"os"
	"sync"
	"time"

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
	sscache          *cache.Cache
	tunnels          *cache.Cache
	pending          *cache.Cache
	wire             net.PacketConn
	wlock            sync.Mutex
	rdbuf            [65536]byte
}

// ObfsListen opens a new obfuscated PacketConn.
func ObfsListen(cookie []byte, wire net.PacketConn) *ObfsSocket {
	return &ObfsSocket{
		cookie:  cookie,
		sscache: cache.New(time.Hour*24, time.Hour),
		tunnels: cache.New(time.Hour*24, time.Hour),
		pending: cache.New(time.Hour, time.Hour),
		wire:    wire,
	}
}

// AddCookieException adds a cookie exception to a particular destination.
func (os *ObfsSocket) AddCookieException(addr net.Addr, cookie []byte) {
	os.cookieExceptions.Store(addr, cookie)
}

func (os *ObfsSocket) WriteTo(b []byte, addr net.Addr) (int, error) {
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
		os.wlock.Lock()
		os.wire.WriteTo(zz.(*prototun).genHello(), addr)
		os.wlock.Unlock()
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
		log.Printf("N4: newproto on %v [%x]", addr.String(), cookie[:4])
	}
	os.pending.SetDefault(addr.String(), pt)
	os.wlock.Lock()
	var err error
	_, err = os.wire.WriteTo(pt.genHello(), addr)
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
		if e == nil {
			os.tunnels.SetDefault(addr.String(), tun)
			n = copy(p, plain)
			if _, ok := os.sscache.Get(string(tun.ss)); ok {
				os.sscache.SetDefault(string(tun.ss), addr)
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
			//log.Println("got bad response to pending", addr, e)
			goto RESTART
		}
		os.tunnels.SetDefault(addr.String(), ts)
		if doLogging {
			log.Println("N4: got realization of pending", addr)
		}
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
			if doLogging {
				log.Println("N4: found a decryptable session through scanning, ROAM to", addr)
			}
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
	pt := newproto(os.cookie)
	ts, e := pt.realize(os.rdbuf[:readBytes], true)
	if e != nil {
		if doLogging {
			log.Println("N4: bad hello from", addr.String(), e.Error())
		}
		goto RESTART
	}
	os.wlock.Lock()
	os.wire.WriteTo(pt.genHello(), addr)
	if doLogging {
		log.Println("N4: responded to a hello from", addr)
	}
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
