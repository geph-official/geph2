package main

import (
	"net"
	"os"
	"time"

	"github.com/geph-official/geph2/libs/cshirt2"
	"github.com/geph-official/geph2/libs/niaucchi4"
	log "github.com/sirupsen/logrus"
)

func mainSingleHop() {
	defer os.Exit(0)
	log.Infoln("<<< ENTERING SINGLE HOP MODE >>>")
	log.Infoln("... All other arguments will be ignored!")
	log.Infof("... PK = %x", pubkey)
	go shUDP()
	shTCP()

}

func shTCP() {
	tcpListener, err := net.Listen("tcp", singleHop)
	if err != nil {
		panic(err)
	}
	log.Infoln("... TCP on", tcpListener.Addr())
	for {
		rawClient, err := tcpListener.Accept()
		if err != nil {
			continue
		}
		log.Debugln("SH client [TCP] @", rawClient.RemoteAddr())
		go func() {
			defer rawClient.Close()
			rawClient.SetDeadline(time.Now().Add(time.Second * 10))
			client, err := cshirt2.Server(pubkey, rawClient)
			if err != nil {
				return
			}
			rawClient.SetDeadline(time.Now().Add(time.Hour * 24))
			handle(client)
		}()
	}
}

func shUDP() {
	udpsock, err := net.ListenPacket("udp", singleHop)
	if err != nil {
		panic(err)
	}
	udpsock.(*net.UDPConn).SetWriteBuffer(10 * 1024 * 1024)
	udpsock.(*net.UDPConn).SetReadBuffer(10 * 1024 * 1024)
	obfs := niaucchi4.ObfsListen(pubkey, udpsock, false)
	log.Infoln("... UDP on", obfs.LocalAddr())
	kcpListener := niaucchi4.ListenKCP(obfs)
	for {
		rc, err := kcpListener.Accept()
		if err != nil {
			log.Println("error while accepting TCP:", err)
			continue
		}
		log.Debugln("SH client [UDP roaming] @", rc.RemoteAddr())
		go handle(rc)
	}
}
