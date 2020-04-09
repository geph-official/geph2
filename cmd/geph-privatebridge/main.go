package main

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"flag"
	"log"
	"net"
	"time"

	"github.com/geph-official/geph2/libs/cshirt2"
	"github.com/google/gops/agent"
	"golang.org/x/time/rate"
)

var generatecookie bool
var cookie []byte
var exitRegex string
var bridgeKey string
var backproxy string
var speedLimit int
var listenAddr string

var limiter *rate.Limiter

func main() {
	flag.BoolVar(&generatecookie, "generatecookie", false, "generate a cookie")
	flag.StringVar(&listenAddr, "listenAddr", ":", "listen address")
	flag.StringVar(&exitRegex, "exitRegex", `\.exits\.geph\.io$`, "domain suffix for exit nodes")
	flag.StringVar(&bridgeKey, "bridgeKey", "", "bridge key")
	flag.StringVar(&backproxy, "backproxy", "", "back proxy SOCKS5 server")
	flag.IntVar(&speedLimit, "speedLimit", -1, "speed limit in KB/s")
	flag.Parse()
	if speedLimit > 0 {
		limiter = rate.NewLimiter(rate.Limit(speedLimit*1024), 10*1000*1000)
	} else {
		limiter = rate.NewLimiter(rate.Inf, 10*1000*1000)
	}
	go func() {
		if err := agent.Listen(agent.Options{}); err != nil {
			log.Fatal(err)
		}
	}()

	if generatecookie {
		cookie = make([]byte, 16)
		rand.Read(cookie)
		log.Println(hex.EncodeToString(cookie))
		return
	}

	var err error
	cookie, err = hex.DecodeString(bridgeKey)
	if err != nil {
		panic(err)
	}

	listenLoop(-1)
}

func listenLoop(deadline time.Duration) {
	listener, err := net.Listen("tcp", listenAddr)
	if err != nil {
		panic(err)
	}
	log.Println("Listen on", listener.Addr())
	if deadline > 0 {
		go func() {
			time.Sleep(deadline)
			listener.Close()
		}()
	}
	defer listener.Close()
	log.Println("TCP listener spinned up")
	for {
		rawClient, err := listener.Accept()
		if err != nil {
			log.Println("CANNOT ACCEPT!", err)
			time.Sleep(time.Millisecond * 100)
			continue
		}
		go func() {
			defer rawClient.Close()
			if !limiter.AllowN(time.Now(), 100*1024) {
				log.Println("rejecting connection because we are already pretty full")
				return
			}
			// intentionally wait in order to deter connections when we're full
			start := time.Now()
			limiter.WaitN(context.Background(), 1000*1000)
			log.Println("delayed opening by", time.Since(start))
			rawClient.SetDeadline(time.Now().Add(time.Second * 10))
			client, err := cshirt2.Server(cookie, rawClient)
			rawClient.SetDeadline(time.Now().Add(time.Hour * 24))
			if err != nil {
				log.Println("cshirt2 failed", err, rawClient.RemoteAddr())
				return
			}
			rawClient.(*net.TCPConn).SetKeepAlive(false)
			//log.Println("Accepted TCP from", rawClient.RemoteAddr())
			handle(client)
		}()
	}
}
