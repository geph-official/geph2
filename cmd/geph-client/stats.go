package main

import (
	"encoding/json"
	"fmt"
	"net/http"
	"runtime"
	"sync"
	"time"

	"github.com/geph-official/geph2/libs/bdclient"
	"github.com/geph-official/geph2/libs/niaucchi4"
)

type stats struct {
	Connected   bool
	PublicIP    string
	UpBytes     uint64
	DownBytes   uint64
	MinPing     uint64
	PingTime    time.Time
	Username    string
	Tier        string
	PayTxes     []bdclient.PaymentTx
	Expiry      time.Time
	LogLines    []string
	Bridges     []niaucchi4.LinkInfo
	bridgeThunk func() []niaucchi4.LinkInfo

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
		if sc.bridgeThunk != nil {
			sc.Bridges = sc.bridgeThunk()
		}
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
		for _, line := range sc.LogLines {
			fmt.Fprintln(w, line)
		}
	})
	w.Header().Add("content-type", "text/plain")
	w.Write(bts)
}

func handleProxyPac(w http.ResponseWriter, r *http.Request) {
	w.Write([]byte(fmt.Sprintf(`function FindProxyForURL(url, host)
	{
		return "PROXY %v";
	}
	`, httpAddr)))
}

func handleStacktrace(w http.ResponseWriter, r *http.Request) {
	buf := make([]byte, 8192)
	for {
		n := runtime.Stack(buf, true)
		if n < len(buf) {
			buf = buf[:n]
			break
		}
		buf = append(buf, buf...)
	}
	w.Write(buf)
}
