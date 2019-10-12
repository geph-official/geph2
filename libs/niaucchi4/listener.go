package niaucchi4

import (
	"net"

	"github.com/geph-official/geph2/libs/kcp-go"
)

// Dial dials KCP over obfs in one function.
func Dial(addr string, cookie []byte) (conn net.Conn, err error) {
	udpsock, err := net.ListenPacket("udp", "")
	if err != nil {
		panic(err)
	}
	obfssock := ObfsListen(cookie, udpsock)
	kcpConn, err := kcp.NewConn(addr, nil, 0, 0, obfssock)
	if err != nil {
		obfssock.Close()
		return
	}
	kcpConn.SetWindowSize(10000, 10000)
	kcpConn.SetNoDelay(0, 50, 3, 0)
	kcpConn.SetStreamMode(true)
	kcpConn.SetMtu(1300)
	conn = kcpConn
	return
}

// Listener operates KCP over obfs. Standard caveats about KCP not having proper open and close signaling apply.
type Listener struct {
	k    *kcp.Listener
	conn *ObfsSocket
}

// Listen creates a new listener.
func Listen(sock *ObfsSocket) *Listener {
	listener, err := kcp.ServeConn(nil, 0, 0, sock)
	if err != nil {
		panic(err)
	}
	return &Listener{
		k:    listener,
		conn: sock,
	}
}

// Accept accepts a new connection.
func (l *Listener) Accept() (c net.Conn, err error) {
	kc, err := l.k.AcceptKCP()
	if err != nil {
		return
	}
	kc.SetWindowSize(10000, 10000)
	kc.SetNoDelay(0, 50, 3, 0)
	kc.SetStreamMode(true)
	kc.SetMtu(1300)
	c = kc
	return
}
