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

	readDeadline  *time.Timer
	writeDeadline *time.Timer

	once sync.Once
}

func newSession() *session {
	return &session{
		rx:  make(chan []byte),
		tx:  make(chan []byte),
		ded: make(chan bool),
		buf: new(bytes.Buffer),
	}
}

func (sess *session) readDeadlineCh() <-chan time.Time {
	if sess.readDeadline != nil {
		return sess.readDeadline.C
	}
	return nil
}

func (sess *session) writeDeadlineCh() <-chan time.Time {
	if sess.writeDeadline != nil {
		return sess.writeDeadline.C
	}
	return nil
}

func (sess *session) Write(pkt []byte) (int, error) {
	cpy := make([]byte, len(pkt))
	copy(cpy, pkt)
	select {
	case sess.tx <- cpy:
		return len(pkt), nil
	case <-sess.writeDeadlineCh():
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
	case <-sess.readDeadlineCh():
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
		if sess.writeDeadline != nil {
			sess.writeDeadline.Stop()
		}
		sess.writeDeadline = time.NewTimer(t.Sub(time.Now()))
	}
	return nil
}

func (sess *session) SetReadDeadline(t time.Time) error {
	if t == (time.Time{}) {
		sess.readDeadline = nil
	} else {
		if sess.readDeadline != nil {
			sess.readDeadline.Stop()
		}
		sess.readDeadline = time.NewTimer(t.Sub(time.Now()))
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
