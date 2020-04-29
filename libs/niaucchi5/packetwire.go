package niaucchi5

import (
	"log"
	"net"
	"time"
)

// PacketWire is the base "unreliable connection" primitive used in niaucchi5.
type PacketWire interface {
	SendSegment(seg []byte, allowBlocking bool) (err error)
	RecvSegment(seg []byte) (n int, err error)
}

type pwAddr struct{}

func (pwAddr) String() string {
	return "packetwire"
}

func (pwAddr) Network() string {
	return "packetwire"
}

// StandardAddr is the standard net.Addr to communicate with the "other end" in PacketWire-generated PacketConns.
var StandardAddr net.Addr

func init() {
	StandardAddr = &pwAddr{}
}

// ToPacketConn converts a PacketWire to a PacketConn with a standard remote address.
func ToPacketConn(pw PacketWire) net.PacketConn {
	return &pwPacketConn{pw}
}

type pwPacketConn struct {
	pwire PacketWire
}

func (pw *pwPacketConn) ReadFrom(p []byte) (n int, addr net.Addr, err error) {
	addr = &pwAddr{}
	n, err = pw.pwire.RecvSegment(p)
	return
}

func (pw *pwPacketConn) WriteTo(p []byte, addr net.Addr) (n int, err error) {
	err = pw.pwire.SendSegment(p, false)
	n = len(p)
	return
}

func (pw *pwPacketConn) Close() (err error) {
	panic("not implemented")
}

func (pw *pwPacketConn) LocalAddr() net.Addr {
	return StandardAddr
}

func (pw *pwPacketConn) RemoteAddr() net.Addr {
	return StandardAddr
}

func (pw *pwPacketConn) SetDeadline(t time.Time) error {
	log.Println("NOT IMPLEMENTED")
	return nil
}

func (pw *pwPacketConn) SetReadDeadline(t time.Time) error {
	log.Println("NOT IMPLEMENTED")
	return nil
}

func (pw *pwPacketConn) SetWriteDeadline(t time.Time) error {
	log.Println("NOT IMPLEMENTED")
	return nil
}
