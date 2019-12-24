package main

import (
	"io/ioutil"
	"log"
	"net"
	"net/http"
	"time"

	"github.com/bunsim/goproxy"
	"github.com/geph-official/geph2/libs/cwl"
	"github.com/geph-official/geph2/libs/tinysocks"
	"golang.org/x/time/rate"
)

func listenStats() {
	// spin up stats server
	statsMux := http.NewServeMux()
	statsServ := &http.Server{
		Addr:         statsAddr,
		Handler:      statsMux,
		ReadTimeout:  time.Minute,
		WriteTimeout: time.Minute,
	}
	statsMux.HandleFunc("/proxy.pac", handleProxyPac)
	statsMux.HandleFunc("/", handleStats)
	statsMux.HandleFunc("/logs", handleLogs)
	statsMux.HandleFunc("/stacktrace", handleStacktrace)
	err := statsServ.ListenAndServe()
	if err != nil {
		panic(err)
	}
}

func listenHTTP() {
	// HTTP proxy
	srv := goproxy.NewProxyHttpServer()
	srv.Tr = &http.Transport{
		Dial: func(n, d string) (net.Conn, error) {
			return dialTun(d)
		},
		IdleConnTimeout: time.Second * 60,
		Proxy:           nil,
	}
	srv.Logger = log.New(ioutil.Discard, "", 0)
	proxServ := &http.Server{
		Addr:        httpAddr,
		Handler:     srv,
		ReadTimeout: time.Minute * 5,
		IdleTimeout: time.Minute * 5,
	}
	err := proxServ.ListenAndServe()
	if err != nil {
		panic(err.Error())
	}
}

func listenSocks() {
	listener, err := net.Listen("tcp", socksAddr)
	if err != nil {
		panic(err)
	}
	semaphore := make(chan bool, 512)
	downLimit := rate.NewLimiter(rate.Inf, 10000000)
	upLimit := rate.NewLimiter(rate.Inf, 10000000)
	useStats(func(sc *stats) {
		if sc.Tier == "free" {
			upLimit = rate.NewLimiter(100*1000, 1000*1000)
		}
	})
	log.Println("SOCKS5 on 9909")
	for {
		cl, err := listener.Accept()
		if err != nil {
			panic(err)
		}
		go func() {
			defer cl.Close()
			select {
			case semaphore <- true:
				defer func() {
					<-semaphore
				}()
			default:
				return
			}
			rmAddr, err := tinysocks.ReadRequest(cl)
			if err != nil {
				return
			}
			start := time.Now()
			remote, ok := sWrap.DialCmd("proxy", rmAddr)
			if !ok {
				return
			}
			defer remote.Close()
			ping := time.Since(start)
			log.Printf("[%v] opened %v in %v", len(semaphore), rmAddr, ping)
			useStats(func(sc *stats) {
				pmil := ping.Milliseconds()
				if time.Since(sc.PingTime).Seconds() > 30 || uint64(pmil) < sc.MinPing {
					sc.MinPing = uint64(pmil)
					sc.PingTime = time.Now()
				}
			})
			if !ok {
				tinysocks.CompleteRequest(5, cl)
				return
			}
			tinysocks.CompleteRequest(0, cl)
			go func() {
				defer remote.Close()
				defer cl.Close()
				cwl.CopyWithLimit(remote, cl, downLimit, func(n int) {
					useStats(func(sc *stats) {
						sc.UpBytes += uint64(n)
					})
				})
			}()
			cwl.CopyWithLimit(cl, remote,
				upLimit, func(n int) {
					useStats(func(sc *stats) {
						sc.DownBytes += uint64(n)
					})
				})
		}()
	}
}
