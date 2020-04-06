package main

import (
	"fmt"
	"log"
	"net"
	"time"

	"github.com/ethereum/go-ethereum/rlp"
	"github.com/geph-official/geph2/libs/cwl"
	"github.com/geph-official/geph2/libs/tinysocks"
	//"github.com/geph-official/geph2/libs/niaucchi4/backedtcp"
)

func handle(client net.Conn) {
	client.SetDeadline(time.Now().Add(time.Minute * 5))
	var err error
	defer func() {
		log.Println("Closed client", client.RemoteAddr(), "reason", err)
	}()
	defer client.Close()
	dec := rlp.NewStream(client, 100000)
	for {
		var command string
		err = dec.Decode(&command)
		if err != nil {
			return
		}
		log.Println("Client", client.RemoteAddr(), "requested", command)
		switch command {
		case "conn/feedback":
			client.SetDeadline(time.Now().Add(time.Hour * 24))
			var host string
			log.Println("waiting for host...")
			err = dec.Decode(&host)
			if err != nil {
				return
			}
			log.Println("host is", host)

			remoteAddr := fmt.Sprintf("%v:2389", host)
			var remote net.Conn

			if backproxy != "" {
				remote, err = net.DialTimeout("tcp", backproxy, time.Second*5)
				if err != nil {
					log.Println("failed to connect to SOCKS5 front proxy server:", err)
					return
				}
				err, _ = tinysocks.Client(remote, tinysocks.ParseAddr(remoteAddr), tinysocks.CmdConnect)
				if err != nil {
					log.Println("failed to shakehand with second socks5 server:", err)
					return
				}
			} else {
				remote, err = net.DialTimeout("tcp", remoteAddr, time.Second*5)
				if err != nil {
					log.Println("failed connecting to", remoteAddr, err)
					return
				}
			}

			log.Println("connected to", remoteAddr, "from", client.RemoteAddr().String())

			err = rlp.Encode(client, uint(0))
			if err != nil {
				log.Println("error feedbacking:", err)
				return
			}

			go func() {
				defer remote.Close()
				defer client.Close()
				cwl.CopyWithLimit(remote, client, nil, nil, time.Hour)
			}()
			defer remote.Close()
			cwl.CopyWithLimit(client, remote, nil, nil, time.Hour)
			return
		default:
			log.Println("unknown command:", command)
			return
		}

	}
}
