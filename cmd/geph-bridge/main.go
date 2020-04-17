package main

import (
	"bytes"
	"crypto/rand"
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
	"github.com/geph-official/geph2/libs/cshirt2"
	"github.com/geph-official/geph2/libs/erand"
	"github.com/geph-official/geph2/libs/kcp-go"
	"github.com/google/gops/agent"
	"github.com/patrickmn/go-cache"
	"golang.org/x/time/rate"
)

var binderFront string
var binderReal string
var exitRegex string
var binderKey string
var statsdAddr string
var allocGroup string
var speedLimit int
var noLegacyUDP bool
var compatibility bool
var wfAddr string
var listenAddr string
var bclient *bdclient.Client
var dummy bool

var limiter *rate.Limiter

var statClient *statsd.StatsdClient

func main() {
	flag.StringVar(&binderFront, "binderFront", "https://binder.geph.io/v2", "binder domain-fronting host")
	flag.StringVar(&binderReal, "binderReal", "binder.geph.io", "real hostname of the binder")
	flag.StringVar(&exitRegex, "exitRegex", `\.exits\.geph\.io$`, "domain suffix for exit nodes")
	flag.StringVar(&statsdAddr, "statsdAddr", "c2.geph.io:8125", "address of StatsD for gathering statistics")
	flag.StringVar(&binderKey, "binderKey", "", "binder API key")
	flag.StringVar(&allocGroup, "allocGroup", "", "allocation group")
	flag.StringVar(&listenAddr, "listenAddr", ":", "listen address")
	flag.BoolVar(&noLegacyUDP, "noLegacyUDP", false, "reject legacy UDP (e2enat) attempts")
	flag.BoolVar(&compatibility, "compatibility", false, "retain compatibility with old cshirt2")
	flag.StringVar(&wfAddr, "wfAddr", "", "if set, listen for plain HTTP warpfront connections on this port. Prevents contacting the binder --- warpfront bridges are manually provisioned!")
	flag.IntVar(&speedLimit, "speedLimit", -1, "speed limit in KB/s")
	flag.BoolVar(&dummy, "dummy", false, "dummy mode")
	flag.Parse()
	if speedLimit > 0 {
		limiter = rate.NewLimiter(rate.Limit(speedLimit*1024), 1000*1000)
	} else {
		limiter = rate.NewLimiter(rate.Inf, 1000*1000)
	}
	go func() {
		if err := agent.Listen(agent.Options{}); err != nil {
			log.Fatal(err)
		}
	}()
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
	if wfAddr != "" {
		log.Println("*** WARPFRONT MODE ***")
		wfLoop()
	}
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
	for i := 0; i < 256; i++ {
		go func() {
			go listenLoop(-1)
		}()
	}
	for {
		time.Sleep(time.Hour)
	}
}

var blacklist = cache.New(time.Hour, time.Hour)

func listenLoop(deadline time.Duration) {
	cookie := make([]byte, 32)
	rand.Read(cookie)
	listener, err := net.Listen("tcp", listenAddr)
	go func() {
		end := time.Now().Add(deadline)
		for deadline < 0 || time.Now().Before(end.Add(-time.Minute*5)) {
			myAddr := fmt.Sprintf("%v:%v", guessIP(), listener.Addr().(*net.TCPAddr).Port)
			e := bclient.AddBridge(binderKey, cookie, myAddr, allocGroup)
			if e != nil {
				log.Println("error adding bridge:", e)
			}
			time.Sleep(time.Minute)
		}
	}()
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
	log.Println("N4/TCP listener spinned up")
	for {
		rawClient, err := listener.Accept()
		if err != nil {
			log.Println("CANNOT ACCEPT!", err)
			time.Sleep(time.Millisecond * 100)
			return
		}
		out := strings.Split(rawClient.RemoteAddr().String(), ":")[0]
		go func() {
			defer rawClient.Close()
			if dummy {
				log.Println(rawClient.RemoteAddr(), "dummy, rejecting", out)
				return
			}
			rawClient.SetDeadline(time.Now().Add(time.Second * time.Duration(15+erand.Int(10))))
			client, err := cshirt2.Server(cookie, compatibility, rawClient)
			rawClient.SetDeadline(time.Now().Add(time.Hour * 24))
			if err != nil {
				log.Println(rawClient.RemoteAddr(), "cshirt2 failed", err)
				return
			}
			rawClient.(*net.TCPConn).SetKeepAlive(false)
			//log.Println("Accepted TCP from", rawClient.RemoteAddr())
			handle(client)
		}()
	}
	// e2e := niaucchi4.ObfsListen(cookie, udpsock, false)
	// if err != nil {
	// 	panic(err)
	// }
	// listener := niaucchi4.ListenKCP(e2e)
	// log.Println("N4/UDP listener spinned up")
	// if deadline > 0 {
	// 	go func() {
	// 		time.Sleep(deadline)
	// 		listener.Close()
	// 	}()
	// }
	// defer listener.Close()
	// for {
	// 	client, err := listener.Accept()
	// 	if err != nil {
	// 		log.Println("CANNOT ACCEPT!", err)
	// 		time.Sleep(time.Millisecond * 100)
	// 		continue
	// 	}
	// 	//log.Println("Accepted UDP client", client.RemoteAddr())
	// 	go handle(client)
	// }
}

func guessIP() string {
retry:
	resp, err := http.Get("https://checkip.amazonaws.com")
	if err != nil {
		log.Println("stuck while getting our own IP:" + err.Error())
		goto retry
	}
	buf := new(bytes.Buffer)
	io.Copy(buf, resp.Body)
	resp.Body.Close()
	myip := strings.Trim(string(buf.Bytes()), "\n ")
	return myip
}
