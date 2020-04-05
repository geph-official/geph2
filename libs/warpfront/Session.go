package warpfront

import (
	"bytes"
	"errors"
	"io"
	"net"
	"sync"
	"time"
)

type session struct {
	rx  chan []byte
	tx  chan []byte
	ded chan bool
	buf *bytes.Buffer

	readDeadline  <-chan time.Time
	writeDeadline <-chan time.Time

	once sync.Once
}

func newSession() *session {
	return &session{
		rx:  make(chan []byte, 1024),
		tx:  make(chan []byte, 1024),
		ded: make(chan bool),
		buf: new(bytes.Buffer),
	}
}

func (sess *session) Write(pkt []byte) (int, error) {
	cpy := make([]byte, len(pkt))
	copy(cpy, pkt)
	select {
	case sess.tx <- cpy:
		return len(pkt), nil
	case <-sess.writeDeadline:
		return 0, errors.New("write timeout")
	case <-sess.ded:
		return 0, io.ErrClosedPipe
	}
}

func (sess *session) Read(p []byte) (int, error) {
	if sess.buf.Len() > 0 {
		return sess.buf.Read(p)
	}
	select {
	case bts := <-sess.rx:
		sess.buf.Write(bts)
		return sess.Read(p)
	case <-sess.readDeadline:
		return 0, errors.New("read timeout")
	case <-sess.ded:
		return 0, io.ErrClosedPipe
	}
}

func (sess *session) Close() error {
	sess.once.Do(func() {
		close(sess.ded)
	})
	return nil
}

func (sess *session) LocalAddr() net.Addr {
	return &wfAddr{}
}

func (sess *session) RemoteAddr() net.Addr {
	return &wfAddr{}
}

func (sess *session) SetDeadline(t time.Time) error {
	sess.SetWriteDeadline(t)
	sess.SetReadDeadline(t)
	return nil
}

func (sess *session) SetWriteDeadline(t time.Time) error {
	if t == (time.Time{}) {
		sess.writeDeadline = nil
	} else {
		sess.writeDeadline = time.After(t.Sub(time.Now()))
	}
	return nil
}

func (sess *session) SetReadDeadline(t time.Time) error {
	if t == (time.Time{}) {
		sess.readDeadline = nil
	} else {
		sess.readDeadline = time.After(t.Sub(time.Now()))
	}
	return nil
}

type wfAddr struct{}

func (wfAddr) String() string {
	return "warpfront"
}

func (wfAddr) Network() string {
	return "warpfront"
}
