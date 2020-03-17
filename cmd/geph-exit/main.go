package main

import (
	"crypto/ed25519"
	"flag"
	"io/ioutil"
	"net"
	"net/http"
	"os"
	"time"

	_ "net/http/pprof"

	statsd "github.com/etsy/statsd/examples/go"
	"github.com/geph-official/geph2/libs/bdclient"
	"github.com/geph-official/geph2/libs/fastudp"
	"github.com/geph-official/geph2/libs/kcp-go"
	"github.com/geph-official/geph2/libs/niaucchi4"
	"github.com/patrickmn/go-cache"
	log "github.com/sirupsen/logrus"
)

var keyfile string
var pubkey ed25519.PublicKey
var seckey ed25519.PrivateKey
var onlyPaid bool

var singleHop string

var binderFront string
var binderReal string
var bclient *bdclient.Client
var hostname string
var statsdAddr string
var speedLimit int

var statClient *statsd.StatsdClient

var ipcache = cache.New(time.Hour, time.Hour)

func main() {

	log.SetFormatter(&log.TextFormatter{
		FullTimestamp: false,
	})
	log.SetLevel(log.DebugLevel)
	flag.StringVar(&keyfile, "keyfile", "keyfile.bin", "location of key file")
	flag.StringVar(&binderFront, "binderFront", "http://binder.geph.io:9080", "binder domain-fronting host")
	flag.StringVar(&binderReal, "binderReal", "binder.geph.io", "real hostname of the binder")
	flag.StringVar(&statsdAddr, "statsdAddr", "c2.geph.io:8125", "address of StatsD for gathering statistics")
	flag.BoolVar(&onlyPaid, "onlyPaid", false, "only allow paying users")
	flag.IntVar(&speedLimit, "speedLimit", 12500, "per-session speed limit, in KB/s")
	flag.StringVar(&singleHop, "singleHop", "", "if supplied, runs in single-hop mode. (for example, -singleHop :5000 would listen on port 5000)")
	flag.Parse()
	go func() {
		log.Println(http.ListenAndServe("localhost:6060", nil))
	}()

	// load the key
	kcp.CongestionControl = "BIC"
	loadKey()
	if singleHop != "" {
		mainSingleHop()
	}
	log.Infof("Loaded PK = %x", pubkey)

	var err error
	hostname, err = os.Hostname()
	if err != nil {
		statsdAddr = ""
	} else {
	}
	if statsdAddr != "" {
		z, e := net.ResolveUDPAddr("udp", statsdAddr)
		if e != nil {
			panic(e)
		}
		statClient = statsd.New(z.IP.String(), z.Port)
	}
	bclient = bdclient.NewClient(binderFront, binderReal)

	// listen
	go func() {
		tcpListener, err := net.Listen("tcp", ":2389")
		if err != nil {
			panic(err)
		}
		log.Infof("Listen on TCP 2389")
		for {
			rawClient, err := tcpListener.Accept()
			if err != nil {
				continue
			}
			go handle(rawClient)
		}
	}()
	go e2elisten()
	udpsock, err := net.ListenPacket("udp4", ":2389")
	if err != nil {
		panic(err)
	}
	udpsock.(*net.UDPConn).SetWriteBuffer(100 * 1024 * 1024)
	udpsock.(*net.UDPConn).SetReadBuffer(100 * 1024 * 1024)
	obfs := niaucchi4.ObfsListen(make([]byte, 32), udpsock, false)
	kcpListener := niaucchi4.ListenKCP(obfs)
	log.Infoln("Listen on UDP 2389")
	for {
		rc, err := kcpListener.Accept()
		if err != nil {
			continue
		}
		rc.SetWindowSize(10000, 10000)
		rc.SetNoDelay(0, 100, 32, 0)
		rc.SetStreamMode(true)
		rc.SetMtu(1300)
		go handle(rc)
	}
}

func e2elisten() {
	udpsock, err := net.ListenPacket("udp4", ":2399")
	if err != nil {
		panic(err)
	}
	fastsock := fastudp.NewConn(udpsock.(*net.UDPConn))
	udpsock.(*net.UDPConn).SetWriteBuffer(100 * 1024 * 1024)
	udpsock.(*net.UDPConn).SetReadBuffer(100 * 1024 * 1024)
	log.Infoln("e2elisten on UDP 2399")
	e2e := niaucchi4.NewE2EConn(fastsock)
	kcpListener := niaucchi4.ListenKCP(e2e)
	for {
		rc, err := kcpListener.Accept()
		if err != nil {
			log.Println("error while accepting E2E:", err)
			continue
		}
		rc.SetWindowSize(10000, 10000)
		rc.SetNoDelay(0, 100, 32, 0)
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
