package fastudp

import "sync"

var bufPool = &sync.Pool{
	New: func() interface{} {
		return make([]byte, 2048)
	},
}

func malloc(n int) []byte {
	return bufPool.Get().([]byte)[:n]
}

func free(bts []byte) {
	bufPool.Put(bts[:2048])
}
