package main

import (
	"bytes"
	"crypto/sha256"
	"flag"
	"fmt"
	"io"
	"log"
	"net"
	"net/http"
	"strings"
	"time"

	statsd "github.com/etsy/statsd/examples/go"
	"github.com/geph-official/geph2/libs/bdclient"
	"github.com/geph-official/geph2/libs/kcp-go"
	"github.com/geph-official/geph2/libs/niaucchi4"
)

var cookieSeed string
var cookie []byte

var binderFront string
var binderReal string
var exitRegex string
var binderKey string
var statsdAddr string
var allocGroup string

var bclient *bdclient.Client

var statClient *statsd.StatsdClient

func main() {
	flag.StringVar(&cookieSeed, "cookieSeed", "", "seed for generating a cookie")
	flag.StringVar(&binderFront, "binderFront", "https://ajax.aspnetcdn.com/v2", "binder domain-fronting host")
	flag.StringVar(&binderReal, "binderReal", "gephbinder.azureedge.net", "real hostname of the binder")
	flag.StringVar(&exitRegex, "exitRegex", `\.exits\.geph\.io$`, "domain suffix for exit nodes")
	flag.StringVar(&statsdAddr, "statsdAddr", "c2.geph.io:8125", "address of StatsD for gathering statistics")
	flag.StringVar(&binderKey, "binderKey", "", "binder API key")
	flag.StringVar(&allocGroup, "allocGroup", "", "allocation group")
	flag.Parse()
	if cookieSeed == "" {
		log.Fatal("must specify a good cookie seed")
	}
	if allocGroup == "" {
		log.Fatal("must specify an allocation group")
	}
	if statsdAddr != "" {
		z, e := net.ResolveUDPAddr("udp", statsdAddr)
		if e != nil {
			panic(e)
		}
		statClient = statsd.New(z.IP.String(), z.Port)
	}
	generateCookie()
	bclient = bdclient.NewClient(binderFront, binderReal)
	go func() {
		lastTotal := uint64(0)
		lastRetrans := uint64(0)
		for {
			time.Sleep(time.Second * 10)
			s := kcp.DefaultSnmp.Copy()
			deltaTotal := float64(s.OutSegs - lastTotal)
			lastTotal = s.OutSegs
			deltaRetrans := float64(s.RetransSegs - lastRetrans)
			lastRetrans = s.RetransSegs
			RL := int64(10000 * deltaRetrans / (deltaRetrans + deltaTotal))
			statClient.Timing(allocGroup+".lossPct", RL)
		}
	}()
	listenLoop()
}

func generateCookie() {
	z := sha256.Sum256([]byte(cookieSeed))
	cookie = z[:]
	log.Printf("Cookie generated: %x", cookie)
}

func listenLoop() {
	udpsock, err := net.ListenPacket("udp", ":")
	if err != nil {
		panic(err)
	}
	udpsock.(*net.UDPConn).SetWriteBuffer(1000 * 1000 * 10)
	myAddr := fmt.Sprintf("%v:%v", guessIP(), udpsock.LocalAddr().(*net.UDPAddr).Port)
	log.Println("server started UDP on", myAddr)
	go func() {
		for {
			e := bclient.AddBridge(binderKey, cookie, myAddr)
			if e != nil {
				log.Println("error adding bridge:", e)
			}
			time.Sleep(time.Minute)
		}
	}()
	e2e := niaucchi4.ObfsListen(cookie, udpsock)
	if err != nil {
		panic(err)
	}
	listener := niaucchi4.ListenKCP(e2e)
	log.Println("KCP listener spinned up")
	for {
		client, err := listener.Accept()
		if err != nil {
			panic(err)
		}
		log.Println("Accepted client", client.RemoteAddr())
		go handle(client)
	}
}

func guessIP() string {
	resp, err := http.Get("https://checkip.amazonaws.com")
	if err != nil {
		panic("stuck while getting our own IP: " + err.Error())
	}
	buf := new(bytes.Buffer)
	io.Copy(buf, resp.Body)
	resp.Body.Close()
	myip := strings.Trim(string(buf.Bytes()), "\n ")
	return myip
}
