package main

import (
	"log"
	"runtime"
	"sync"
)

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

var e2ejobs = make(chan func(), 100*1024)

func maybeDoJob(f func()) {
	// select {
	// case e2ejobs <- f:
	// default:
	// }
	f()
}

func init() {
	for i := 0; i < runtime.GOMAXPROCS(0); i++ {
		workerID := i
		log.Println("spawning worker thread", workerID)
		go func() {
			for i := 0; ; i++ {
				if i%1000 == 0 {
					log.Println("worker", workerID, "forwarded", i, "packets")
				}
				(<-e2ejobs)()
			}
		}()
	}
}
