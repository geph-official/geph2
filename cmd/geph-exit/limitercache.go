package main

import (
	lru "github.com/hashicorp/golang-lru"
	"golang.org/x/time/rate"
)

type limitFactory struct {
	lcache *lru.Cache
	limit  rate.Limit
}

func (lf *limitFactory) getLimiter(sessid [32]byte) *rate.Limiter {
	new := rate.NewLimiter(lf.limit, 1000*1024)
	prev, _, _ := lf.lcache.PeekOrAdd(sessid, new)
	if prev != nil {
		return prev.(*rate.Limiter)
	} else {
		return new
	}
}

func newLimitFactory(limit rate.Limit) *limitFactory {
	l, _ := lru.New(65536)
	return &limitFactory{
		limit:  limit,
		lcache: l,
	}
}

var slowLimitFactory = newLimitFactory(100 * 1024)
var fastLimitFactory *limitFactory
