package main

import (
	"sync"
	"sync/atomic"
)

// net.Conn => *int64
var trackerMap sync.Map

func getCounter(key interface{}) *int64 {
	rv, _ := trackerMap.LoadOrStore(key, new(int64))
	return rv.(*int64)
}

func incrCounter(key interface{}) {
	atomic.AddInt64(getCounter(key), 1)
}

func decrCounter(key interface{}) {
	atomic.AddInt64(getCounter(key), -1)
}

func readCounter(key interface{}) {
	atomic.LoadInt64(getCounter(key))
}
