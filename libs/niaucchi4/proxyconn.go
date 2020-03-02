package niaucchi4

import (
	"log"
	"net"
	"time"

	"github.com/geph-official/geph2/libs/tinysocks"
)

// ProxyConn wraps an underlying UDPConn and batches stuff to it.
type ProxyConn struct {
	sock  *net.UDPConn
	netpr net.Addr
}

// NewProxyConn creates a new ProxyConn.
func NewProxyConn(conn net.PacketConn, localaddr string) net.PacketConn {
	tgtUDPAddr, err := net.ResolveUDPAddr("udp", localaddr)
	if err != nil {
		panic(err)
	}
	return &ProxyConn{
		sock:  conn.(*net.UDPConn),
		netpr: tgtUDPAddr,
	}
}

// ReadFrom reads a packet from the connection.
func (conn *ProxyConn) ReadFrom(p []byte) (n int, addr net.Addr, err error) {
	no, _, err := conn.sock.ReadFrom(p)
	if err != nil {
		log.Println("conn.sock.ReadFrom error:", err)
		return
	}

	//log.Println("ProxyConn read len", no, string(p[:no]), raddr.String())

	rd := tinysocks.SplitAddr(p[3:])
	srcTxt := rd.String()

	//log.Println("ProxyConn readtext", string(p[3+len(rd):no]), "rd", rd)

	copy(p[:], p[3+len(rd):no])

	addr, err = net.ResolveUDPAddr("udp", srcTxt)
	if err != nil {
		panic(err)
	}

	n = no - 3 - len(rd)
	return
}

// WriteTo writes to a given address.
func (conn *ProxyConn) WriteTo(p []byte, addr net.Addr) (n int, err error) {

	baddr := tinysocks.ParseAddr(addr.String())
	payload := append([]byte{5, 0, 0}, baddr...)
	payload = append(payload, p...)
	//log.Println("ProxyConn payload length WriteTo:", len(payload), conn.netpr.String())

	n, err = conn.sock.WriteTo(payload, conn.netpr)
	n = n - 3 - len(baddr)
	return
}

// Close closes the connection.
func (conn *ProxyConn) Close() error {
	err := conn.sock.Close()
	return err
}

// LocalAddr returns the local address.
func (conn *ProxyConn) LocalAddr() net.Addr {
	return conn.sock.LocalAddr()
}

// SetDeadline sets a deadline.
func (conn *ProxyConn) SetDeadline(t time.Time) error {
	return conn.sock.SetDeadline(t)
}

// SetReadDeadline sets a read deadline.
func (conn *ProxyConn) SetReadDeadline(t time.Time) error {
	return conn.sock.SetReadDeadline(t)
}

// SetWriteDeadline sets a write deadline.
func (conn *ProxyConn) SetWriteDeadline(t time.Time) error {
	return conn.sock.SetWriteDeadline(t)
}

// RemoteAddr returns the remote address.
func (conn *ProxyConn) RemoteAddr() net.Addr {
	return conn.sock.RemoteAddr()
}
