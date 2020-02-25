package cwl

import (
	"context"
	. "io"
	"net"
	"time"

	"golang.org/x/time/rate"
)

// CopyWithLimit is like io.Copy but subject to a rate limit and calling a callback.
func CopyWithLimit(dst Writer, src net.Conn, limiter *rate.Limiter, callback func(int), idleTimeout time.Duration) (n int, err error) {
	var buf []byte
	if buf == nil {
		size := 32 * 1024
		buf = make([]byte, size)
	}
	for {
		if idleTimeout > 0 {
			src.SetReadDeadline(time.Now().Add(idleTimeout))
		}
		nr, er := src.Read(buf)
		if nr > 0 {
			if callback != nil {
				callback(nr)
			}
			if limiter != nil {
				limiter.WaitN(context.Background(), nr)
			}
			nw, ew := dst.Write(buf[0:nr])
			if nw > 0 {
				n += nw
			}
			if ew != nil {
				err = ew
				break
			}
			if nr != nw {
				err = ErrShortWrite
				break
			}
		}
		if er != nil {
			if er != EOF {
				err = er
			}
			break
		}
	}
	return
}
