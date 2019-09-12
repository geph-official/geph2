package main

import (
	"encoding/hex"
	"flag"
	"io"
	"log"
	"net"

	"github.com/geph-official/geph2/libs/niaucchi3"
	"github.com/xtaci/kcp-go"
)

var cookie, _ = hex.DecodeString("7b9ac53240d4eba7a22df5917e6a03fe80fcd10f3a11fff3c49f95002be12c0a")

func main() {
	var flagClient string
	var flagServer string
	flag.StringVar(&flagClient, "c", "", "client connect")
	flag.StringVar(&flagServer, "s", "", "server forward")
	flag.Parse()
	if flagClient == "" && flagServer == "" {
		log.Fatal("must give -c or -s")
	}
	if flagClient != "" && flagServer != "" {
		log.Fatal("cannot give both -c or -s")
	}
	// client
	if flagClient != "" {
		listener, err := net.Listen("tcp", "localhost:9909")
		if err != nil {
			panic(err)
		}
		log.Println("TCP listener started on", listener.Addr())
		for {
			client, err := listener.Accept()
			if err != nil {
				panic(err)
			}
			go func() {
				defer client.Close()
				udpsock, err := net.ListenPacket("udp", "")
				if err != nil {
					panic(err)
				}
				e2e := niaucchi3.E2EListen(niaucchi3.ObfsListen(cookie, udpsock))
				remote, err := kcp.NewConn(flagClient, nil, 0, 0, e2e)
				if err != nil {
					panic(err)
				}
				remote.SetWindowSize(10240, 10240)
				remote.SetNoDelay(1, 40, 0, 0)
				defer remote.Close()
				go func() {
					defer remote.Close()
					defer client.Close()
					_, err := io.Copy(remote, client)
					if err != nil {
						log.Println("error copying client to remote:", err)
					}
				}()
				_, err = io.Copy(client, remote)
				if err != nil {
					log.Println("error copying remote to client:", err)
				}
			}()
		}
	}
	// server
	if flagServer != "" {
		udpsock, err := net.ListenPacket("udp", ":19999")
		if err != nil {
			panic(err)
		}
		log.Println("server started UDP on", udpsock.LocalAddr())
		e2e := niaucchi3.E2EListen(niaucchi3.ObfsListen(cookie, udpsock))
		if err != nil {
			panic(err)
		}
		listener, err := kcp.ServeConn(nil, 0, 0, e2e)
		if err != nil {
			panic(err)
		}
		log.Println("KCP forward server started!")
		for {
			client, err := listener.AcceptKCP()
			if err != nil {
				panic(err)
			}
			client.SetWindowSize(10240, 10240)
			client.SetNoDelay(1, 40, 0, 0)
			log.Println("accepted!")
			go func() {
				defer client.Close()
				remote, err := net.Dial("tcp", flagServer)
				if err != nil {
					log.Println("can't connect to remote:", err)
					return
				}
				defer remote.Close()
				defer client.Close()
				defer remote.Close()
				go func() {
					defer remote.Close()
					defer client.Close()
					_, err := io.Copy(remote, client)
					if err != nil {
						log.Println("error copying client to remote:", err)
					}
				}()
				_, err = io.Copy(client, remote)
				if err != nil {
					log.Println("error copying remote to client:", err)
				}
			}()
		}
	}
}
