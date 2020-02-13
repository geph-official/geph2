package niaucchi4

import (
	"log"
	"net"

	"github.com/geph-official/geph2/libs/fastudp"
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
		udpsock.(*net.UDPConn).SetReadBuffer(10 * 1024 * 1024)
		udpsock.(*net.UDPConn).SetWriteBuffer(10 * 1024 * 1024)
		return fastudp.NewConn(udpsock.(*net.UDPConn))
	})
	kcpConn, err := kcp.NewConn(addr, nil, 16, 16, ObfsListen(cookie, socket))
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
	conn net.PacketConn
}

// ListenKCP creates a new listener.
func ListenKCP(sock net.PacketConn) *KCPListener {
	listener, err := kcp.ServeConn(nil, 16, 16, sock)
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
	kc.SetMtu(1300)
	c = kc
	return
}

// Close closes the thing.
func (l *KCPListener) Close() error {
	l.conn.Close()
	return l.k.Close()
}
