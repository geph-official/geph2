package main

import (
	"fmt"
	"log"
	mrand "math/rand"
	"net"
	"regexp"
	"time"

	"github.com/ethereum/go-ethereum/rlp"
	"github.com/geph-official/geph2/libs/cwl"
	"github.com/geph-official/geph2/libs/kcp-go"
	"github.com/geph-official/geph2/libs/pseudotcp"
	//"github.com/geph-official/geph2/libs/niaucchi4/backedtcp"
)

func handle(client net.Conn) {
	// log.Println("***DUMMY***")
	// time.Sleep(time.Minute)
	// return
	client.SetDeadline(time.Now().Add(time.Minute * 5))
	var err error
	defer client.Close()
	exitMatcher, err := regexp.Compile(exitRegex)
	if err != nil {
		panic(err)
	}
	dec := rlp.NewStream(client, 100000)
	for {
		var command string
		err = dec.Decode(&command)
		if err != nil {
			return
		}
		log.Println(client.RemoteAddr(), "requested", command)
		switch command {
		case "conn/e2e":
			if noLegacyUDP {
				return
			}
			var host string
			err = dec.Decode(&host)
			if err != nil {
				return
			}
			if !exitMatcher.MatchString(host) {
				err = fmt.Errorf("bad pattern: %v", host)
				return
			}
			var cookie []byte
			err = dec.Decode(&cookie)
			if err != nil {
				return
			}
			port, err := e2enat(fmt.Sprintf("%v:2399", host), cookie)
			if err != nil {
				log.Println("cannot e2enat:", err)
				return
			}
			log.Printf("created e2enat at port %v (%x)", port, cookie[:10])
			rlp.Encode(client, uint(port))
			time.Sleep(time.Second * 20)
			return
		case "ping":
			rlp.Encode(client, "ping")
			time.Sleep(time.Second)
			return
		case "ping/repeat":
			rlp.Encode(client, "ping")
		case "conn":
			fallthrough
		case "conn/feedback":
			var host string
			err = dec.Decode(&host)
			if err != nil {
				return
			}
			if !exitMatcher.MatchString(host) {
				err = fmt.Errorf("bad pattern: %v", host)
				return
			}
			remoteAddr := fmt.Sprintf("%v:12389", host)
			var remote net.Conn
			remote, err = pseudotcp.Dial(remoteAddr)
			if err != nil {
				log.Println("failed connecting to", remoteAddr, err)
				return
			}
			log.Println(client.RemoteAddr(), "==>", remoteAddr)
			if command == "conn/feedback" {
				err = rlp.Encode(client, uint(0))
				if err != nil {
					log.Println("error feedbacking:", err)
					return
				}
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
						case <-time.After(time.Millisecond * time.Duration(mrand.ExpFloat64()*3000)):
							c, ok := client.(*kcp.UDPSession)
							if ok {
								btlBw, latency, _ := c.FlowStats()
								statClient.Timing(allocGroup+".clientLatency", int64(latency))
								statClient.Timing(allocGroup+".btlBw", int64(btlBw))
							}
						}
					}
				}()
			}
			client.SetDeadline(time.Now().Add(time.Hour * 24))
			go func() {
				defer remote.Close()
				defer client.Close()
				cwl.CopyWithLimit(remote, client, limiter, func(n int) {
					if statClient != nil && mrand.Int()%100000 < n {
						statClient.Increment(allocGroup + ".e2eup")
					}
				}, time.Hour)
			}()
			defer remote.Close()
			cwl.CopyWithLimit(client, remote, limiter, func(n int) {
				if statClient != nil && mrand.Int()%100000 < n {
					statClient.Increment(allocGroup + ".e2edown")
				}
			}, time.Hour)
			return
		default:
			return
		}

	}
}
