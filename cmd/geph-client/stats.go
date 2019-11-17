package main

import (
	"encoding/json"
	"fmt"
	"net/http"
	"sync"
	"time"

	"github.com/geph-official/geph2/libs/bdclient"
)

type stats struct {
	Connected bool
	PublicIP  string
	UpBytes   uint64
	DownBytes uint64
	MinPing   uint64
	PingTime  time.Time
	Username  string
	Tier      string
	PayTxes   []bdclient.PaymentTx
	Expiry    time.Time
	LogLines  []string

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
		ll := sc.LogLines
		sc.LogLines = nil
		var err error
		bts, err = json.Marshal(sc)
		if err != nil {
			panic(err)
		}
		sc.LogLines = ll
	})
	w.Header().Add("content-type", "application/json")
	w.Write(bts)
}

func handleLogs(w http.ResponseWriter, r *http.Request) {
	w.Header().Add("Access-Control-Allow-Origin", "*")
	var bts []byte
	useStats(func(sc *stats) {
		var err error
		bts, err = json.Marshal(sc.LogLines)
		if err != nil {
			panic(err)
		}
	})
	w.Header().Add("content-type", "application/json")
	w.Write(bts)
}

func handleProxyPac(w http.ResponseWriter, r *http.Request) {
	w.Write([]byte(fmt.Sprintf(`function FindProxyForURL(url, host)
	{
		return "PROXY %v";
	}
	`, httpAddr)))
}
