package main

import (
	"bytes"
	"crypto/sha256"
	"flag"
	"fmt"
	"io"
	"log"
	"math/rand"
	"net"
	"net/http"
	"regexp"
	"strings"
	"time"

	"github.com/ethereum/go-ethereum/rlp"
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
		for {
			time.Sleep(time.Second * 1)
			RL := int64(kcp.DefaultSnmp.RecentLoss() * 10000)
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
			time.Sleep(time.Minute * 10)
		}
	}()
	e2e := niaucchi4.ObfsListen(cookie, udpsock)
	if err != nil {
		panic(err)
	}
	listener := niaucchi4.Listen(e2e)
	log.Println("KCP listener spinned up")
	// utilities
	exitMatcher, err := regexp.Compile(exitRegex)
	if err != nil {
		panic(err)
	}
	for {
		client, err := listener.Accept()
		if err != nil {
			panic(err)
		}
		log.Println("Accepted client", client.RemoteAddr())
		go func() {
			var err error
			defer func() {
				log.Println("Closed client", client.RemoteAddr(), "reason", err)
			}()
			defer client.Close()
			for {
				var command string
				rlp.Decode(client, &command)
				log.Println("Client", client.RemoteAddr(), "requested", command)
				switch command {
				case "ping":
					rlp.Encode(client, "ping")
					return
				case "ping/repeat":
					rlp.Encode(client, "ping")
				case "conn":
					fallthrough
				case "conn/feedback":
					var host string
					err = rlp.Decode(client, &host)
					if err != nil {
						return
					}
					if !exitMatcher.MatchString(host) {
						err = fmt.Errorf("bad pattern: %v", host)
						return
					}
					remoteAddr := fmt.Sprintf("%v:2389", host)
					var remote net.Conn
					remote, err = net.Dial("tcp", remoteAddr)
					if err != nil {
						return
					}
					log.Println("connected to", remoteAddr)
					if command == "conn/feedback" {
						rlp.Encode(client, uint(0))
					}
					// report stats in the background
					if statClient != nil {
						statsDone := make(chan bool)
						defer func() {
							close(statsDone)
						}()
						go func() {
							for {
								select {
								case <-statsDone:
									return
								case <-time.After(time.Millisecond * time.Duration(rand.ExpFloat64()*3000)):
									btlBw, latency, _ := client.FlowStats()
									statClient.Timing(allocGroup+".clientLatency", int64(latency))
									statClient.Timing(allocGroup+".btlBw", int64(btlBw))
								}
							}
						}()
					}
					go func() {
						defer remote.Close()
						defer client.Close()
						io.Copy(remote, client)
					}()
					defer remote.Close()
					io.Copy(client, remote)
					return
				}
			}
		}()
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
