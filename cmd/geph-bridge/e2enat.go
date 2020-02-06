package main

import (
	"context"
	"log"
	"math/rand"
	"net"
	"sync"
	"time"

	"github.com/ethereum/go-ethereum/rlp"
	"github.com/geph-official/geph2/libs/fastudp"
	"github.com/geph-official/geph2/libs/niaucchi4"
	"github.com/minio/highwayhash"
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
	return highwayhash.Sum64(pkt.Session[:], key[:])
}

func e2enat(dest string, cookie []byte) (port int, err error) {
	leftRaw, err := net.ListenPacket("udp", "")
	if err != nil {
		return
	}
	leftSock := niaucchi4.ObfsListen(cookie, fastudp.NewConn(leftRaw.(*net.UDPConn)))
	rightSock, err := net.ListenPacket("udp", "")
	if err != nil {
		return
	}
	leftRaw.(*net.UDPConn).SetReadBuffer(1000 * 1024)
	leftRaw.(*net.UDPConn).SetWriteBuffer(1000 * 1024)
	rightSock.(*net.UDPConn).SetWriteBuffer(1000 * 1024)
	rightSock.(*net.UDPConn).SetReadBuffer(1000 * 1024)
	destReal, err := net.ResolveUDPAddr("udp", dest)
	if err != nil {
		return
	}
	rightSock = fastudp.NewConn(rightSock.(*net.UDPConn))
	// mapping
	sessMap := new(sync.Map)
	go func() {
		defer leftSock.Close()
		defer rightSock.Close()
		bts := make([]byte, 2048)
		for {
			dl := time.Now().Add(time.Minute * 20)
			leftSock.SetReadDeadline(dl)
			n, addr, err := leftSock.ReadFrom(bts)
			if err != nil {
				return
			}
			rightSock.SetWriteDeadline(dl)
			sid := parseSess(bts[:n])
			sessMap.Store(sid, addr)
			_, err = rightSock.WriteTo(bts[:n], destReal)
			if err != nil {
				log.Println("cannot write:", err)
			}
			if statClient != nil && rand.Int()%100000 < n {
				statClient.Increment(allocGroup + ".e2eup")
			}
		}
	}()
	go func() {
		defer leftSock.Close()
		defer rightSock.Close()
		bts := make([]byte, 2048)
		for {
			dl := time.Now().Add(time.Minute * 20)
			rightSock.SetReadDeadline(dl)
			n, _, e := rightSock.ReadFrom(bts)
			if e != nil {
				return
			}
			leftSock.SetWriteDeadline(dl)
			sid := parseSess(bts[:n])
			if addri, ok := sessMap.Load(sid); ok {
				bigLimiter.WaitN(context.Background(), n)
				limiter.WaitN(context.Background(), n)
				_, e = leftSock.WriteTo(bts[:n], addri.(net.Addr))
				if err != nil {
					log.Println("cannot write:", err)
				}
			}
			if statClient != nil && rand.Int()%100000 < n {
				statClient.Increment(allocGroup + ".e2edown")
			}
		}
	}()
	return leftRaw.LocalAddr().(*net.UDPAddr).Port, nil
}
