package main

import (
	"crypto/ed25519"
	"flag"
	"io/ioutil"
	"log"
	"net"
	"os"
	"time"

	statsd "github.com/etsy/statsd/examples/go"
	"github.com/geph-official/geph2/libs/bdclient"
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
	udpsock, err := net.ListenPacket("udp", ":2389")
	if err != nil {
		panic(err)
	}
	udpsock.(*net.UDPConn).SetWriteBuffer(10 * 1024 * 1024)
	obfs := niaucchi4.ObfsListen(make([]byte, 32), udpsock)
	if err != nil {
		panic(err)
	}
	kcpListener := niaucchi4.Listen(obfs)
	for {
		rc, err := kcpListener.Accept()
		if err != nil {
			log.Println("error while accepting TCP:", err)
			continue
		}
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
