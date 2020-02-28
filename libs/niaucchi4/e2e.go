package niaucchi4

import (
	"bytes"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"log"
	"net"
	"time"

	"github.com/ethereum/go-ethereum/rlp"
	"github.com/patrickmn/go-cache"
)

// SessionAddr is a net.Addr that represents the ultimate counterparty in an e2e session.
type SessionAddr [16]byte

// NewSessAddr generates a new random sess addr
func NewSessAddr() SessionAddr {
	var k SessionAddr
	rand.Read(k[:])
	return k
}

func (sa SessionAddr) String() string {
	return fmt.Sprintf("SA-%v", hex.EncodeToString(sa[:]))
}

// Network fulfills net.Addr
func (sa SessionAddr) Network() string {
	return "SESS"
}

type pcReadResult struct {
	Contents []byte
	Host     net.Addr
}

// E2EConn is a PacketConn implementing the E2E protocol.
type E2EConn struct {
	sidToSess *cache.Cache
	readQueue chan pcReadResult
	wire      net.PacketConn
	pktbuf    [2048]byte
	Closed    bool
}

// NewE2EConn creates a new e2e connection.
func NewE2EConn(wire net.PacketConn) *E2EConn {
	return &E2EConn{
		sidToSess: cache.New(time.Hour, time.Hour),
		readQueue: make(chan pcReadResult, 1024),
		wire:      wire,
	}
}

func (e2e *E2EConn) DebugInfo() [][]LinkInfo {
	var tr [][]LinkInfo
	for _, v := range e2e.sidToSess.Items() {
		if !v.Expired() {
			tr = append(tr, v.Object.(*e2eSession).DebugInfo())
		}
	}
	return tr
}

// SetSessPath is used by clients to have certain sessions go along certain paths.
func (e2e *E2EConn) SetSessPath(sid SessionAddr, host net.Addr) {
	var sess *e2eSession
	if sessi, ok := e2e.sidToSess.Get(sid.String()); ok {
		sess = sessi.(*e2eSession)
	} else {
		sess = newSession(sid)
	}
	e2e.sidToSess.SetDefault(sid.String(), sess)
	sess.AddPath(host)
}

// ReadFrom implements PacketConn.
func (e2e *E2EConn) ReadFrom(p []byte) (n int, from net.Addr, err error) {
	for {
		select {
		case prr := <-e2e.readQueue:
			n = copy(p, prr.Contents)
			from = prr.Host
			return
		default:
			err = e2e.readOnePacket()
			if err != nil {
				return
			}
		}
	}
}

// WriteTo implements PacketConn.
func (e2e *E2EConn) WriteTo(p []byte, addr net.Addr) (n int, err error) {
	sessid := addr.(SessionAddr)
	sessi, ok := e2e.sidToSess.Get(sessid.String())
	if !ok {
		err = errors.New("session is dead")
		return
	}
	sess := sessi.(*e2eSession)
	e2e.sidToSess.SetDefault(sessid.String(), sess)
	err = sess.Send(p, func(toSend e2ePacket, dest net.Addr) {
		buffer := malloc(2048)
		defer free(buffer)
		buf := bytes.NewBuffer(buffer[:0])
		err = rlp.Encode(buf, toSend)
		if err != nil {
			panic(err)
		}
		e2e.wire.WriteTo(buf.Bytes(), dest)
	})
	if err != nil {
		return
	}
	return
}

// UnderlyingLoss returns the underlying loss.
func (e2e *E2EConn) UnderlyingLoss(destAddr net.Addr) (frac float64) {
	sessid := destAddr.(SessionAddr)
	sessi, ok := e2e.sidToSess.Get(sessid.String())
	if !ok {
		log.Println("cannot find underlying loss")
		return
	}
	sess := sessi.(*e2eSession)
	sess.lock.Lock()
	defer sess.lock.Unlock()
	rem := sess.info[sess.lastRemid]
	frac = rem.remoteLoss
	return
}

// Close closes the underlying socket.
func (e2e *E2EConn) Close() error {
	e2e.Closed = true
	return e2e.wire.Close()
}

// LocalAddr returns the local address.
func (e2e *E2EConn) LocalAddr() net.Addr {
	return e2e.wire.LocalAddr()
}

// SetDeadline lala
func (e2e *E2EConn) SetDeadline(t time.Time) error {
	return e2e.wire.SetDeadline(t)
}

// SetWriteDeadline lala
func (e2e *E2EConn) SetWriteDeadline(t time.Time) error {
	return e2e.wire.SetDeadline(t)
}

// SetReadDeadline lala
func (e2e *E2EConn) SetReadDeadline(t time.Time) error {
	return e2e.wire.SetDeadline(t)
}

func (e2e *E2EConn) readOnePacket() error {
	n, from, err := e2e.wire.ReadFrom(e2e.pktbuf[:])
	if err != nil {
		return err
	}
	var pkt e2ePacket
	err = rlp.DecodeBytes(e2e.pktbuf[:n], &pkt)
	if err != nil {
		return err
	}
	var sess *e2eSession
	if sessi, ok := e2e.sidToSess.Get(pkt.Session.String()); ok {
		sess = sessi.(*e2eSession)
	} else {
		sess = newSession(pkt.Session)
	}
	e2e.sidToSess.SetDefault(pkt.Session.String(), sess)
	sess.AddPath(from)
	sess.Input(pkt, from)
	sess.FlushReadQueue(func(b []byte) {
		select {
		case e2e.readQueue <- pcReadResult{b, pkt.Session}:
		default:
			if doLogging {
				log.Println("N4: dropping packet of size", len(b), "because overfull e2e.readQueue")
			}
		}
	})
	return nil
}
