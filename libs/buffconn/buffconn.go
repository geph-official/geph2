package buffconn

import (
	"bufio"
	"net"
	"time"
)

// BuffConn is a net.Conn that implements read buffering to save on syscalls.
type BuffConn struct {
	wire      net.Conn
	bufReader *bufio.Reader
}

// New creates a new BuffConn.
func New(wire net.Conn) *BuffConn {
	return &BuffConn{
		wire:      wire,
		bufReader: bufio.NewReaderSize(wire, 65536),
	}
}

func (bc *BuffConn) Read(p []byte) (int, error) {
	return bc.bufReader.Read(p)
}

func (bc *BuffConn) Write(p []byte) (int, error) {
	return bc.wire.Write(p)
}

func (bc *BuffConn) Close() error {
	return bc.wire.Close()
}

func (bc *BuffConn) SetDeadline(t time.Time) error {
	return bc.wire.SetDeadline(t)
}

func (bc *BuffConn) SetReadDeadline(t time.Time) error {
	return bc.wire.SetReadDeadline(t)
}

func (bc *BuffConn) SetWriteDeadline(t time.Time) error {
	return bc.wire.SetWriteDeadline(t)
}

func (bc *BuffConn) LocalAddr() net.Addr {
	return bc.wire.LocalAddr()
}

func (bc *BuffConn) RemoteAddr() net.Addr {
	return bc.wire.RemoteAddr()
}
