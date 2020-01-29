package fastudp

import (
	"io"
	"net"
	"time"

	"golang.org/x/net/ipv4"
	"gopkg.in/tomb.v1"
)

// Conn wraps an underlying UDPConn and batches stuff to it.
type Conn struct {
	sock  *net.UDPConn
	pconn *ipv4.PacketConn
	death *tomb.Tomb

	writeBuf chan ipv4.Message
	readBuf  []ipv4.Message
}

// NewConn creates a new Conn.
func NewConn(conn *net.UDPConn) *Conn {
	c := &Conn{
		sock:     conn,
		pconn:    ipv4.NewPacketConn(conn),
		writeBuf: make(chan ipv4.Message, 256),
		death:    new(tomb.Tomb),
	}
	go c.bkgWrite()
	return c
}

func (conn *Conn) bkgWrite() {
	defer conn.pconn.Close()
	defer conn.sock.Close()
	var towrite []ipv4.Message
	for {
		select {
		case first := <-conn.writeBuf:
			towrite = append(towrite, first)
			for len(towrite) < 1024 {
				select {
				case next := <-conn.writeBuf:
					towrite = append(towrite, next)
				default:
					goto out
				}
			}
		out:
			ptr := towrite
			for len(ptr) > 0 {
				n, err := conn.pconn.WriteBatch(ptr, 0)
				if err != nil {
					conn.death.Kill(err)
					return
				}
				for i := 0; i < n; i++ {
					free(ptr[i].Buffers[0])
					ptr[i].Buffers = nil
				}
				ptr = ptr[n:]
			}
			towrite = towrite[:0]
		case <-conn.death.Dying():
			return
		}
	}
}

// ReadFrom reads a packet from the connection.
func (conn *Conn) ReadFrom(p []byte) (n int, addr net.Addr, err error) {
	// TODO batch this too
	n, addr, err = conn.sock.ReadFrom(p)
	if err != nil {
		conn.death.Kill(err)
	}
	return
}

// WriteTo writes to a given address.
func (conn *Conn) WriteTo(p []byte, addr net.Addr) (n int, err error) {
	pCopy := malloc(len(p))
	copy(pCopy, p)
	msg := ipv4.Message{
		Buffers: [][]byte{pCopy},
		Addr:    addr,
	}
	select {
	case conn.writeBuf <- msg:
	default:
	}
	return len(p), nil
}

// Close closes the connection.
func (conn *Conn) Close() error {
	err := conn.sock.Close()
	conn.death.Kill(io.ErrClosedPipe)
	return err
}

// SetDeadline sets a deadline.
func (conn *Conn) SetDeadline(t time.Time) error {
	return conn.sock.SetDeadline(t)
}

// SetReadDeadline sets a read deadline.
func (conn *Conn) SetReadDeadline(t time.Time) error {
	return conn.sock.SetReadDeadline(t)
}

// SetWriteDeadline sets a write deadline.
func (conn *Conn) SetWriteDeadline(t time.Time) error {
	return conn.sock.SetWriteDeadline(t)
}

// LocalAddr returns the local address.
func (conn *Conn) LocalAddr() net.Addr {
	return conn.sock.LocalAddr()
}

// RemoteAddr returns the remote address.
func (conn *Conn) RemoteAddr() net.Addr {
	return conn.sock.RemoteAddr()
}
