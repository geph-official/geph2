package niaucchi4

import (
	"log"
	"net"

	"github.com/geph-official/geph2/libs/kcp-go"
)

// DialKCP dials KCP over obfs in one function.
func DialKCP(addr string, cookie []byte) (conn net.Conn, err error) {
	socket := Wrap(func() net.PacketConn {
		udpsock, err := net.ListenPacket("udp", "")
		if err != nil {
			panic(err)
		}
		if doLogging {
			log.Println("N4: recreating source socket", udpsock.LocalAddr())
		}
		return udpsock
	})
	kcpConn, err := kcp.NewConn(addr, nil, 0, 0, ObfsListen(cookie, socket))
	if err != nil {
		socket.Close()
		return
	}
	kcpConn.SetWindowSize(1000, 10000)
	kcpConn.SetNoDelay(0, 50, 3, 0)
	kcpConn.SetStreamMode(true)
	kcpConn.SetMtu(1300)
	conn = kcpConn
	return
}

// KCPListener operates KCP over obfs. Standard caveats about KCP not having proper open and close signaling apply.
type KCPListener struct {
	k    *kcp.Listener
	conn *ObfsSocket
}

// ListenKCP creates a new listener.
func ListenKCP(sock *ObfsSocket) *KCPListener {
	listener, err := kcp.ServeConn(nil, 0, 0, sock)
	if err != nil {
		panic(err)
	}
	return &KCPListener{
		k:    listener,
		conn: sock,
	}
}

// Accept accepts a new connection.
func (l *KCPListener) Accept() (c *kcp.UDPSession, err error) {
	kc, err := l.k.AcceptKCP()
	if err != nil {
		return
	}
	kc.SetWindowSize(10000, 1000)
	kc.SetNoDelay(0, 50, 2, 0)
	kc.SetStreamMode(true)
	kc.SetMtu(1300)
	c = kc
	return
}
