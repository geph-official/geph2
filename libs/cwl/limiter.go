package cwl

import (
	"context"
	. "io"

	"golang.org/x/time/rate"
)

// CopyWithLimit is like io.Copy but subject to a rate limit and calling a callback.
func CopyWithLimit(dst Writer, src Reader, limiter *rate.Limiter, callback func(int)) (n int, err error) {
	var buf []byte
	if buf == nil {
		size := 32 * 1024
		if l, ok := src.(*LimitedReader); ok && int64(size) > l.N {
			if l.N < 1 {
				size = 1
			} else {
				size = int(l.N)
			}
		}
		buf = make([]byte, size)
	}
	for {
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
