package main

import (
	"flag"
	"io"
	"io/ioutil"
	"log"
	"net"

	"github.com/geph-official/geph2/libs/kcp-go"
	"github.com/geph-official/geph2/libs/niaucchi5"
	"github.com/google/gops/agent"
)

func main() {
	if err := agent.Listen(agent.Options{}); err != nil {
		log.Fatal(err)
	}

	var flagClient string
	var flagServer string
	flag.StringVar(&flagClient, "c", "", "client connect")
	flag.StringVar(&flagServer, "s", "", "server listen")
	flag.Parse()

	if flagClient == "" && flagServer == "" {
		log.Fatal("must give -c or -s")
	}
	if flagClient != "" && flagServer != "" {
		log.Fatal("cannot give both -c or -s")
	}
	if flagServer != "" {
		mainServer(flagServer)
	}
	if flagClient != "" {
		mainClient(flagClient)
	}
}

func mainServer(flagServer string) {
	listener, err := net.Listen("tcp", flagServer)
	if err != nil {
		panic(err)
	}
	for {
		cl, err := listener.Accept()
		if err != nil {
			panic(err)
		}
		go func() {
			defer cl.Close()
			ur := niaucchi5.NewURTCP(cl)
			pc := niaucchi5.ToPacketConn(ur)
			k, err := kcp.NewConn2(niaucchi5.StandardAddr, nil, 0, 0, pc)
			if err != nil {
				panic(err)
			}
			k.SetWindowSize(10000, 10000)
			k.SetNoDelay(0, 100, 3, 0)
			k.SetStreamMode(true)
			k.SetMtu(1200)

			for {
				_, err := k.Write(make([]byte, 1400))
				if err != nil {
					log.Println(err)
					return
				}
			}
		}()
	}
}

func mainClient(flagClient string) {
	conn, err := net.Dial("tcp", flagClient)
	if err != nil {
		panic(err)
	}
	ur := niaucchi5.NewURTCP(conn)
	pc := niaucchi5.ToPacketConn(ur)
	k, err := kcp.NewConn2(niaucchi5.StandardAddr, nil, 0, 0, pc)
	if err != nil {
		panic(err)
	}
	k.SetWindowSize(10000, 10000)
	k.SetNoDelay(0, 100, 3, 0)
	k.SetStreamMode(true)
	k.SetMtu(1200)
	_, err = io.Copy(ioutil.Discard, k)
	if err != nil {
		panic(err)
	}
}
