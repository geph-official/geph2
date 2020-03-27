package main

import (
	lru "github.com/hashicorp/golang-lru"
	"golang.org/x/time/rate"
)

var lcache, _ = lru.New(16384)

func getLimiter(sessid [32]byte) *rate.Limiter {
	new := rate.NewLimiter(100*1024, 1000*1024)
	prev, _, _ := lcache.PeekOrAdd(sessid, new)
	if prev != nil {
		return prev.(*rate.Limiter)
	} else {
		return new
	}
}
