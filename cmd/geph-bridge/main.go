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

	"github.com/ethereum/go-ethereum/rlp"
	"github.com/geph-official/geph2/libs/bdclient"
	"github.com/geph-official/geph2/libs/kcp-go"
	"github.com/geph-official/geph2/libs/niaucchi4"
)

var cookieSeed string
var cookie []byte

var binderFront string
var binderReal string
var exitDomain string

var bclient *bdclient.Client

func main() {
	flag.StringVar(&cookieSeed, "cookieSeed", "", "seed for generating a cookie")
	flag.StringVar(&binderFront, "binderFront", "http://binder.geph.io:9080", "binder domain-fronting host")
	flag.StringVar(&binderReal, "binderReal", "binder.geph.io", "real hostname of the binder")
	flag.StringVar(&exitDomain, "exitDomain", ".exits.geph.io", "domain suffix for exit nodes")
	flag.Parse()
	if cookieSeed == "" {
		log.Fatal("must specify a good cookie seed")
	}
	generateCookie()
	bclient = bdclient.NewClient(binderFront, binderReal)
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
			e := bclient.AddBridge(cookie, myAddr)
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
	listener, err := kcp.ServeConn(nil, 0, 0, e2e)
	if err != nil {
		panic(err)
	}
	log.Println("KCP listener spinned up")
	go func() {
		for {
			time.Sleep(time.Second * 5)
			rt := kcp.DefaultSnmp.RetransSegs
			tot := kcp.DefaultSnmp.OutPkts
			log.Printf("%2.f pct retrans, %v recovered", float64(rt)/float64(tot)*100,
				kcp.DefaultSnmp.FECRecovered)
		}
	}()
	for {
		client, err := listener.AcceptKCP()
		if err != nil {
			panic(err)
		}
		log.Println("Accepted client", client.RemoteAddr())
		go func() {
			defer log.Println("Closed client", client.RemoteAddr())
			defer client.Close()
			client.SetWindowSize(10000, 10000)
			client.SetNoDelay(1, 10, 3, 0)
			client.SetStreamMode(true)
			client.SetMtu(1350)
			var command string
			rlp.Decode(client, &command)
			log.Println("Client", client.RemoteAddr(), "requested", command)
			switch command {
			case "ping":
				rlp.Encode(client, "ping")
				return
			case "conn":
				var host string
				err := rlp.Decode(client, &host)
				if err != nil {
					return
				}
				if !strings.HasSuffix(host, exitDomain) {
					return
				}
				remoteAddr := fmt.Sprintf("%v:2389", host)
				remote, err := net.Dial("tcp", remoteAddr)
				if err != nil {
					return
				}
				log.Println("KCP", client.RemoteAddr(), "=> TCP", remoteAddr)
				go func() {
					defer remote.Close()
					defer client.Close()
					io.Copy(remote, client)
				}()
				defer remote.Close()
				io.Copy(client, remote)
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
