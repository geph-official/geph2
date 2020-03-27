package main

import (
	"sync"
	"sync/atomic"
)

// string => *int64
var trackerMap sync.Map

func getCounter(key string) *int64 {
	rv, _ := trackerMap.LoadOrStore(key, new(int64))
	return rv.(*int64)
}

func incrCounter(key string) {
	atomic.AddInt64(getCounter(key), 1)
}

func decrCounter(key string) {
	atomic.AddInt64(getCounter(key), -1)
}

func readCounter(key string) {
	atomic.LoadInt64(getCounter(key))
}
