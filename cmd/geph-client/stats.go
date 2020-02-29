package main

import (
	"archive/zip"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"runtime"
	"runtime/pprof"
	"sync"
	"time"

	"github.com/geph-official/geph2/libs/bdclient"
	"github.com/geph-official/geph2/libs/niaucchi4"
	log "github.com/sirupsen/logrus"
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

func handleKill(w http.ResponseWriter, r *http.Request) {
	log.Println("dying on command")
	go func() {
		time.Sleep(time.Millisecond * 100)
		os.Exit(0)
	}()
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

func handleDebugPack(w http.ResponseWriter, r *http.Request) {
	w.Header().Add("Content-Disposition", fmt.Sprintf("attachment; filename=\"geph-logs-%v-%v.zip\"", username, time.Now().Format(time.RFC3339)))
	w.Header().Add("content-type", "application/zip")
	zwriter := zip.NewWriter(w)
	defer zwriter.Close()
	logFile, err := zwriter.Create("logs.txt")
	if err != nil {
		return
	}
	useStats(func(sc *stats) {
		for _, line := range sc.LogLines {
			fmt.Fprintln(logFile, line)
		}
	})
	straceFile, err := zwriter.Create("stacktrace.txt")
	if err != nil {
		return
	}
	buf := make([]byte, 8192)
	for {
		n := runtime.Stack(buf, true)
		if n < len(buf) {
			buf = buf[:n]
			break
		}
		buf = append(buf, buf...)
	}
	straceFile.Write(buf)
	heapprofFile, err := zwriter.Create("heap.pprof")
	pprof.WriteHeapProfile(heapprofFile)
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
