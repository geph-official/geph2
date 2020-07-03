package warpfront

import (
	"bytes"
	"encoding/binary"
	"io"
	"net"
	"net/http"
	"sync"
	"time"
)

// Server wraps around the packet server
type Server struct {
	sessions map[string]*session
	seshch   chan *session
	dedch    chan bool

	once sync.Once
	sync.Mutex
}

// NewServer creates a http.Handler for warpfront.
func NewServer() *Server {
	return &Server{
		sessions: make(map[string]*session),
		seshch:   make(chan *session),
	}
}

// Close destroys the warpfront context.
func (srv *Server) Close() error {
	srv.once.Do(func() {
		close(srv.dedch)
	})
	return nil
}

// Accept accepts a warpfront session.
func (srv *Server) Accept() (net.Conn, error) {
	select {
	case sesh := <-srv.seshch:
		return sesh, nil
		// TODO somehow do a big error thing?
	case <-srv.dedch:
		return nil, io.ErrClosedPipe
	}
}

func (srv *Server) destroySession(key string) {
	srv.Lock()
	defer srv.Unlock()
	chs, ok := srv.sessions[key]
	if ok {
		chs.Close()
		delete(srv.sessions, key)
	}
}

func (srv *Server) handleDelete(wr http.ResponseWriter, rq *http.Request) {
	wr.Header().Set("cache-control", "no-cache")
	sesh := rq.URL.Query().Get("id")
	if sesh == "" {
		wr.WriteHeader(http.StatusBadRequest)
		return
	}
	srv.destroySession(sesh)
}

func (srv *Server) handleRegister(wr http.ResponseWriter, rq *http.Request) {
	sesh := rq.URL.Query().Get("id")
	if sesh == "" {
		wr.WriteHeader(http.StatusBadRequest)
		return
	}
	wr.Header().Set("cache-control", "no-cache")
	srv.Lock()
	_, ok := srv.sessions[sesh]
	// reject if already exists
	if ok {
		wr.WriteHeader(http.StatusForbidden)
		srv.Unlock()
		return
	}
	// otherwise, we initialize
	chs := newSession()
	srv.sessions[sesh] = chs
	srv.Unlock()
	// now we feed into the big chan
	select {
	case srv.seshch <- chs:
		wr.WriteHeader(http.StatusOK)
		go func() {
			<-chs.ded
			srv.destroySession(sesh)
		}()
	case <-time.After(time.Second * 1):
		srv.destroySession(sesh)
		wr.WriteHeader(http.StatusInternalServerError)
	}
}

// ServeHTTP implements the basic stuff for ppServ
func (srv *Server) ServeHTTP(wr http.ResponseWriter, rq *http.Request) {
	key := rq.URL.Path[1:]

	if key == "register" {
		srv.handleRegister(wr, rq)
		return
	}

	if key == "delete" {
		srv.handleDelete(wr, rq)
		return
	}

	// query for the session
	srv.Lock()
	chs, ok := srv.sessions[key]
	srv.Unlock()

	if !ok {
		wr.WriteHeader(http.StatusBadRequest)
		return
	}

	up, dn, ded := chs.rx, chs.tx, chs.ded

	wr.Header().Set("Content-Encoding", "application/octet-stream")
	wr.Header().Set("Cache-Control", "no-cache, no-store")

	// signal for continuing
	contbuf := make([]byte, 4)
	binary.BigEndian.PutUint32(contbuf, 0)

	switch rq.Method {
	case "GET":
		ctr := 0
		start := time.Now()
		for ctr < 10*1024*1024 && time.Now().Sub(start) < time.Second*40 {
			delay := time.Millisecond * 5000
			select {
			case bts := <-dn:
				// write length, then bytes
				buf := make([]byte, 4)
				binary.BigEndian.PutUint32(buf, uint32(len(bts)))
				_, err := wr.Write(append(buf, bts...))
				if err != nil {
					srv.destroySession(key)
					return
				}
				ctr += len(bts)
				wr.(http.Flusher).Flush()
				delay = time.Millisecond * 5000
			case <-time.After(delay):
				wr.Write(contbuf)
				wr.(http.Flusher).Flush()
				delay = delay + time.Millisecond*50
				if delay > time.Second*5 {
					delay = time.Second * 5
				}
				return
			case <-ded:
				srv.destroySession(key)
				return
			}
		}
		wr.Write(contbuf)
		wr.(http.Flusher).Flush()
		time.Sleep(time.Second)
	case "POST":
		pkrd := new(bytes.Buffer)
		_, err := io.Copy(pkrd, rq.Body)
		if err != nil {
			srv.destroySession(key)
			return
		}
		select {
		case up <- pkrd.Bytes(): // TODO potential deadlock; currently mitigated by a buffer
			return
		case <-time.After(time.Minute):
			srv.destroySession(key)
			return
		case <-ded:
			srv.destroySession(key)
			return
		}
	}
}
