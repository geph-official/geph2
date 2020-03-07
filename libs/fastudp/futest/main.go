package main

import (
	"fmt"
	"log"
	"net"
	"time"

	"github.com/geph-official/geph2/libs/fastudp"
	"github.com/geph-official/geph2/libs/niaucchi4"
	"github.com/google/gops/agent"
)

func main() {
	go func() {
		if err := agent.Listen(agent.Options{}); err != nil {
			log.Fatal(err)
		}
		for {
			time.Sleep(time.Hour)
		}
	}()

	go func() {
		socket, err := net.ListenPacket("udp", ":")
		if err != nil {
			panic(err)
		}
		socket = fastudp.NewConn(socket.(*net.UDPConn))
		osocket := niaucchi4.ObfsListen(nil, socket, false)
		go func() {
			for {
				osocket.ReadFrom(make([]byte, 10000))
			}
		}()
		destAddr, err := net.ResolveUDPAddr("udp", "localhost:11111")
		fsPkt := make([]byte, 1400)
		for {
			_, err = osocket.WriteTo(fsPkt, destAddr)
			if err != nil {
				panic(err)
			}
			//time.Sleep(time.Second)
		}
	}()

	socket, err := net.ListenPacket("udp", ":11111")
	if err != nil {
		panic(err)
	}
	socket = fastudp.NewConn(socket.(*net.UDPConn))
	osocket := niaucchi4.ObfsListen(nil, socket, false)
	//socket.(*net.UDPConn).SetWriteBuffer(1000 * 1000 * 100)
	//fastSocket := fastudp.NewConn(socket.(*net.UDPConn))

	if err != nil {
		panic(err)
	}
	start := time.Now()
	bts := make([]byte, 10000)
	for i := 0; ; i++ {
		//log.Println(i)
		osocket.ReadFrom(bts)
		if i%10000 == 0 {
			elapsed := time.Since(start)
			speed := float64(i) / elapsed.Seconds()
			fmt.Println("received", i, "packets in", elapsed)
			fmt.Printf("\t(%.2f pp/s)\n", speed)
		}
	}
}
