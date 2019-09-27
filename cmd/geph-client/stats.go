package main

import (
	"encoding/json"
	"net/http"
	"sync"
	"time"
)

type stats struct {
	Connected bool
	PublicIP  string
	UpBytes   uint64
	DownBytes uint64
	Username  string
	Tier      string
	Expiry    time.Time

	lock sync.Mutex
}

var statsCollector = &stats{}

func useStats(f func(sc *stats)) {
	statsCollector.lock.Lock()
	defer statsCollector.lock.Unlock()
	f(statsCollector)
}

func handleStats(w http.ResponseWriter, r *http.Request) {
	w.Header().Add("Access-Control-Allow-Origin", "*")
	var bts []byte
	useStats(func(sc *stats) {
		var err error
		bts, err = json.Marshal(sc)
		if err != nil {
			panic(err)
		}
	})
	w.Header().Add("content-type", "application/json")
	w.Write(bts)
}
