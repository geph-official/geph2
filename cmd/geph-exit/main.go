package main

import (
	"crypto/ed25519"
	"flag"
	"io/ioutil"
	"log"
	"net"
	"net/http"
	"os"
	"time"

	_ "net/http/pprof"

	statsd "github.com/etsy/statsd/examples/go"
	"github.com/geph-official/geph2/libs/bdclient"
	"github.com/geph-official/geph2/libs/kcp-go"
	"github.com/geph-official/geph2/libs/niaucchi4"
	"github.com/patrickmn/go-cache"
)

var keyfile string
var pubkey ed25519.PublicKey
var seckey ed25519.PrivateKey
var onlyPaid bool

var binderFront string
var binderReal string
var bclient *bdclient.Client
var hostname string
var statsdAddr string

var statClient *statsd.StatsdClient

var ipcache = cache.New(time.Hour, time.Hour)

func main() {
	flag.StringVar(&keyfile, "keyfile", "keyfile.bin", "location of key file")
	flag.StringVar(&binderFront, "binderFront", "http://binder.geph.io:9080", "binder domain-fronting host")
	flag.StringVar(&binderReal, "binderReal", "binder.geph.io", "real hostname of the binder")
	flag.StringVar(&statsdAddr, "statsdAddr", "c2.geph.io:8125", "address of StatsD for gathering statistics")
	flag.BoolVar(&onlyPaid, "onlyPaid", false, "only allow paying users")
	flag.Parse()

	go func() {
		log.Println(http.ListenAndServe("localhost:6060", nil))
	}()

	var err error
	hostname, err = os.Hostname()
	if err != nil {
		statsdAddr = ""
	} else {
		log.Println(hostname)
	}
	if statsdAddr != "" {
		z, e := net.ResolveUDPAddr("udp", statsdAddr)
		if e != nil {
			panic(e)
		}
		statClient = statsd.New(z.IP.String(), z.Port)
		log.Println("created statClient!")
	}
	bclient = bdclient.NewClient(binderFront, binderReal)

	// load the key
	loadKey()
	log.Printf("Loaded PK = %x", pubkey)
	// listen
	go func() {
		tcpListener, err := net.Listen("tcp", ":2389")
		if err != nil {
			panic(err)
		}
		log.Println("Started MUX on TinySS on port 2389")
		for {
			rawClient, err := tcpListener.Accept()
			if err != nil {
				log.Println("error while accepting TCP:", err)
				continue
			}
			go handle(rawClient)
		}
	}()
	go e2elisten()
	udpsock, err := net.ListenPacket("udp", ":2389")
	if err != nil {
		panic(err)
	}
	udpsock.(*net.UDPConn).SetWriteBuffer(10 * 1024 * 1024)
	obfs := niaucchi4.ObfsListen(make([]byte, 32), udpsock)
	if err != nil {
		panic(err)
	}
	kcpListener := niaucchi4.ListenKCP(obfs)
	for {
		rc, err := kcpListener.Accept()
		if err != nil {
			log.Println("error while accepting TCP:", err)
			continue
		}
		go handle(rc)
	}
}

func e2elisten() {
	udpsock, err := net.ListenPacket("udp", ":2399")
	if err != nil {
		panic(err)
	}
	log.Println("e2elisten on 2399")
	udpsock.(*net.UDPConn).SetWriteBuffer(10 * 1024 * 1024)
	e2e := niaucchi4.NewE2EConn(udpsock)
	kcpListener := niaucchi4.ListenKCP(e2e)
	for {
		rc, err := kcpListener.Accept()
		if err != nil {
			log.Println("error while accepting E2E:", err)
			continue
		}
		kcp.QuiescentMax = 1 << 30
		rc.SetWindowSize(10000, 1000)
		rc.SetNoDelay(0, 50, 3, 0)
		rc.SetStreamMode(true)
		rc.SetMtu(1300)
		go handle(rc)
	}
}

func loadKey() {
retry:
	bts, err := ioutil.ReadFile(keyfile)
	if err != nil {
		// genkey
		_, key, _ := ed25519.GenerateKey(nil)
		ioutil.WriteFile(keyfile, key, 0600)
		goto retry
	}
	seckey = bts
	pubkey = seckey.Public().(ed25519.PublicKey)
}
