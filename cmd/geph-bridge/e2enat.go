package main

import (
	"context"
	"encoding/binary"
	"fmt"
	"log"
	"math/rand"
	"net"
	"sync"
	"sync/atomic"
	"time"

	"github.com/ethereum/go-ethereum/rlp"
	"github.com/geph-official/geph2/libs/fastudp"
	"github.com/geph-official/geph2/libs/niaucchi4"
	"github.com/patrickmn/go-cache"
	"golang.org/x/time/rate"
)

type e2ePacket struct {
	Session niaucchi4.SessionAddr
	Sn      uint64
	Ack     uint64
	Body    []byte
	Padding []byte
}

var key [32]byte

func parseSess(bts []byte) uint64 {
	var pkt e2ePacket
	rlp.DecodeBytes(bts, &pkt)
	return binary.BigEndian.Uint64(pkt.Session[:8])
}

var e2ecount int64

func init() {
	go func() {
		for {
			if statClient != nil {
				statClient.Send(map[string]string{
					allocGroup + ".e2eCount": fmt.Sprintf("%v|g", atomic.LoadInt64(&e2ecount)),
				}, 1)
			}
			time.Sleep(time.Second * 10)
		}
	}()
}

var e2eMap = cache.New(time.Hour, time.Hour)
var e2eMapLk sync.Mutex

func e2enat(dest string, cookie []byte) (port int, err error) {
	// e2eMapLk.Lock()
	// defer e2eMapLk.Unlock()
	// log.Println("e2enat", atomic.LoadInt64(&e2ecount))
	// kee := fmt.Sprintf("%v/%x", dest, cookie)
	// if porti, ok := e2eMap.Get(kee); ok {
	// 	log.Println("HIT", kee)
	// 	port = porti.(int)
	// 	return
	// }
	// log.Println("MISS", kee)
	leftRaw, err := net.ListenPacket("udp", "")
	if err != nil {
		return
	}
	leftRaw = fastudp.NewConn(leftRaw.(*net.UDPConn))
	leftSock := niaucchi4.ObfsListen(cookie, leftRaw, true)
	rightSock, err := net.ListenPacket("udp", "")
	if err != nil {
		return
	}
	destReal, err := net.ResolveUDPAddr("udp", dest)
	if err != nil {
		return
	}
	rightSock = fastudp.NewConn(rightSock.(*net.UDPConn))
	// mapping
	sessMap := new(sync.Map)
	go func() {
		atomic.AddInt64(&e2ecount, 1)
		defer atomic.AddInt64(&e2ecount, -1)
		defer leftSock.Close()
		defer rightSock.Close()
		bts := malloc(2048)
		for {
			dl := time.Now().Add(time.Minute * 30)
			leftSock.SetReadDeadline(dl)
			n, addr, err := leftSock.ReadFrom(bts)
			if err != nil {
				log.Println("closing", leftRaw.LocalAddr(), err)
				return
			}
			sid := parseSess(bts[:n])
			sessMap.Store(sid, addr)
			btsCopy := malloc(n)
			copy(btsCopy, bts)
			maybeDoJob(func() {
				_, err = rightSock.WriteTo(btsCopy, destReal)
				if err != nil {
					log.Println("cannot write:", err)
				}
				if statClient != nil && rand.Int()%100000 < n {
					statClient.Increment(allocGroup + ".e2eup")
				}
				free(btsCopy)
			})
		}
	}()
	go func() {
		defer leftSock.Close()
		defer rightSock.Close()
		bts := malloc(2048)
		for {
			dl := time.Now().Add(time.Minute * 30)
			rightSock.SetReadDeadline(dl)
			n, _, e := rightSock.ReadFrom(bts)
			if e != nil {
				log.Println("closing", rightSock.LocalAddr(), err)
				return
			}
			leftSock.SetWriteDeadline(dl)
			sid := parseSess(bts[:n])
			if addri, ok := sessMap.Load(sid); ok {
				btsCopy := malloc(n)
				copy(btsCopy, bts)
				start := time.Now()
				maybeDoJob(func() {
					limiter.WaitN(context.Background(), n)
					_, e = leftSock.WriteTo(btsCopy, addri.(net.Addr))
					if err != nil {
						log.Println("cannot write:", err)
					}
					free(btsCopy)
					if statClient != nil {
						inducedLatency := time.Since(start)
						if rand.Int()%100000 < n {
							statClient.Increment(allocGroup + ".e2edown")
						}
						if queueReportLimiter.Allow() {
							statClient.Timing(allocGroup+".queuens", inducedLatency.Nanoseconds())
						}
					}
				})
			}
		}
	}()
	//e2eMap.SetDefault(kee, leftRaw.LocalAddr().(*net.UDPAddr).Port)
	return leftRaw.LocalAddr().(*net.UDPAddr).Port, nil
}

var queueReportLimiter = rate.NewLimiter(100, 1000)
