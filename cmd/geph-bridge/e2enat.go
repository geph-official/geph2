package main

import (
	"context"
	"log"
	"math/rand"
	"net"
	"time"

	"github.com/geph-official/geph2/libs/fastudp"
	"github.com/geph-official/geph2/libs/niaucchi4"
)

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
	var laddr net.Addr
	go func() {
		defer leftSock.Close()
		defer rightSock.Close()
		bts := make([]byte, 2048)
		for {
			dl := time.Now().Add(time.Hour)
			leftSock.SetReadDeadline(dl)
			n, addr, err := leftSock.ReadFrom(bts)
			if err != nil {
				return
			}
			laddr = addr
			rightSock.SetWriteDeadline(dl)
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
			dl := time.Now().Add(time.Hour)
			rightSock.SetReadDeadline(dl)
			n, _, e := rightSock.ReadFrom(bts)
			if e != nil {
				return
			}
			leftSock.SetWriteDeadline(dl)
			if laddr != nil {
				bigLimiter.WaitN(context.Background(), n)
				limiter.WaitN(context.Background(), n)
				_, e = leftSock.WriteTo(bts[:n], laddr)
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
