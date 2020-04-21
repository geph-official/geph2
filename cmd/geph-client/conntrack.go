package main

import (
	"net"
	"sync"
	"sync/atomic"
)

// net.Conn => *int64
var trackerMap sync.Map

func getCounter(key net.Conn) *int64 {
	rv, _ := trackerMap.LoadOrStore(key, new(int64))
	return rv.(*int64)
}

func incrCounter(key net.Conn) {
	atomic.AddInt64(getCounter(key), 1)
}

func decrCounter(key net.Conn) {
	atomic.AddInt64(getCounter(key), -1)
}

func readCounter(key net.Conn) {
	atomic.LoadInt64(getCounter(key))
}
