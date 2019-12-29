package main

import (
	"context"
	"flag"
	"io"
	"log"
	"net"
	"sync/atomic"
	"time"

	"github.com/geph-official/geph2/libs/kcp-go"
	"github.com/geph-official/geph2/libs/niaucchi4"
	"golang.org/x/time/rate"
)

func main() {
	var flagClient string
	var flagServer string
	var flagConAlgo string
	var flagLimit int
	flag.StringVar(&flagClient, "c", "", "client connect")
	flag.StringVar(&flagServer, "s", "", "server listen")
	flag.StringVar(&flagConAlgo, "cc", "LOL", "congestion control algorithm")
	flag.IntVar(&flagLimit, "l", -1, "speed limit")
	flag.Parse()
	kcp.CongestionControl = flagConAlgo

	if flagClient == "" && flagServer == "" {
		log.Fatal("must give -c or -s")
	}
	if flagClient != "" && flagServer != "" {
		log.Fatal("cannot give both -c or -s")
	}
	if flagServer != "" {
		mainServer(flagServer, flagLimit)
	}
	if flagClient != "" {
		mainClient(flagClient)
	}
}

func mainClient(dialto string) {
	udpsock, err := net.ListenPacket("udp", "")
	if err != nil {
		panic(err)
	}
	servAddr, err := net.ResolveUDPAddr("udp", dialto)
	if err != nil {
		panic(err)
	}
	kcpremote, err := kcp.NewConn2(servAddr, nil, 0, 0, niaucchi4.ObfsListen(nil, udpsock))
	if err != nil {
		panic(err)
	}
	defer kcpremote.Close()
	kcpremote.SetWindowSize(10000, 10000)
	kcpremote.SetNoDelay(0, 50, 3, 0)
	kcpremote.SetStreamMode(true)
	kcpremote.SetMtu(1200)
	kcpremote.Write([]byte("HELLO"))
	var kbs uint64
	go func() {
		buf := make([]byte, 1024)
		for {
			_, err := io.ReadFull(kcpremote, buf)
			if err != nil {
				panic(err)
			}
			atomic.AddUint64(&kbs, 1)
		}
	}()
	last := uint64(0)
	for {
		time.Sleep(time.Second)
		rn := atomic.LoadUint64(&kbs)
		log.Println("Current speed:", rn-last, "KiB/s")
		last = rn
	}
}

func mainServer(listen string, klimit int) {
	var limiter *rate.Limiter
	if klimit > 0 {
		limiter = rate.NewLimiter(rate.Limit(klimit*1024), 1024*1024)
	}
	udpsock, err := net.ListenPacket("udp", listen)
	if err != nil {
		panic(err)
	}
	obfs := niaucchi4.ObfsListen(nil, udpsock)
	if err != nil {
		panic(err)
	}
	listener, err := kcp.ServeConn(nil, 0, 0, obfs)
	if err != nil {
		panic(err)
	}
	log.Println("KCP over N4 listener spinned up!")
	for {
		kclient, err := listener.AcceptKCP()
		if err != nil {
			panic(err)
		}
		log.Println("Accepted kclient from", kclient.RemoteAddr())
		kclient.SetWindowSize(10000, 10000)
		kclient.SetNoDelay(0, 10, 3, 0)
		kclient.SetStreamMode(true)
		kclient.SetMtu(1200)
		go func() {
			defer kclient.Close()
			buf := make([]byte, 5)
			io.ReadFull(kclient, buf)
			for {
				if limiter != nil {
					limiter.WaitN(context.Background(), 65536)
				}
				_, err := kclient.Write(make([]byte, 65536))
				if err != nil {
					return
				}
			}
		}()
	}
}
