package connswarm

import (
	"bytes"
	"io"
	"log"
	"net"
	"time"

	"github.com/ethereum/go-ethereum/rlp"
	tomb "gopkg.in/tomb.v1"
)

// NilAddr is a dummy net.Addr.
type NilAddr struct{}

func (na NilAddr) String() string {
	return "NIL"
}

// Network implements net.Addr
func (na NilAddr) Network() string {
	return "NIL"
}

// Swarm represents a swarm of reliable(-ish) connections that simulates an unreliable transport.
type Swarm struct {
	writeQueue chan []byte
	readQueue  chan []byte
	readBuf    bytes.Buffer
	remoteAddr net.Addr
	dead       *tomb.Tomb
}

// NewSwarm creates a new swarm.
func NewSwarm() *Swarm {
	return &Swarm{
		writeQueue: make(chan []byte, 10),
		readQueue:  make(chan []byte, 10),
		remoteAddr: NilAddr{},
		dead:       new(tomb.Tomb),
	}
}

// Close implements net.PacketConn
func (sw *Swarm) Close() error {
	sw.dead.Kill(io.ErrClosedPipe)
	return nil
}

// LocalAddr implements net.PacketConn
func (sw *Swarm) LocalAddr() net.Addr {
	return NilAddr{}
}

// SetDeadline implements net.PacketConn
func (sw *Swarm) SetDeadline(time.Time) error {
	return nil
}

// SetReadDeadline implements net.PacketConn
func (sw *Swarm) SetReadDeadline(time.Time) error {
	return nil
}

// SetWriteDeadline implements net.PacketConn
func (sw *Swarm) SetWriteDeadline(time.Time) error {
	return nil
}

// ReadFrom implements net.PacketConn
func (sw *Swarm) ReadFrom(p []byte) (n int, addr net.Addr, err error) {
	select {
	case <-sw.dead.Dead():
		err = io.ErrClosedPipe
		return
	case bts := <-sw.readQueue:
		sw.readBuf.Write(bts)
		n, err = sw.readBuf.Read(p)
		addr = sw.remoteAddr
		return
	}
}

// WriteTo implements net.PacketConn
func (sw *Swarm) WriteTo(p []byte, addr net.Addr) (n int, err error) {
	log.Println("writing to", addr)
	sw.remoteAddr = addr
	pcopy := make([]byte, len(p))
	copy(pcopy, p)
	select {
	case <-sw.dead.Dead():
		err = io.ErrClosedPipe
		return
	case sw.writeQueue <- pcopy:
		return
	default:
		log.Println("write queue overflowed")
		return
	}
}

// AddConn adds a net.Conn to the swarm to last for a certain amount of time.
func (sw *Swarm) AddConn(conn net.Conn, duration time.Duration) {
	log.Println("adding for", duration)
	conn.SetDeadline(time.Now().Add(duration))
	// read thread
	go func() {
		timeout := time.After(duration)
		defer conn.Close()
		for {
			var pkt []byte
			err := rlp.Decode(conn, &pkt)
			if err != nil {
				log.Println("error in rlp:", err)
				return
			}
			log.Println("decoded", len(pkt))
			select {
			case sw.readQueue <- pkt:
			case <-timeout:
				log.Println("read thread timed out")
				return
			default:
				log.Println("read queue overflowed, discarding pkt")
			}
		}
	}()
	// write thread
	go func() {
		timeout := time.After(duration)
		defer conn.Close()
		for {
			select {
			case pkt := <-sw.writeQueue:
				log.Println("encoding", len(pkt))
				rlp.Encode(conn, pkt)
			case <-timeout:
				log.Println("write thread timed out")
				return
			case <-sw.dead.Dead():
				log.Println("dead!")
				return
			}
		}
	}()
}
