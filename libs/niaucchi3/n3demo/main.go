package main

import (
	"context"
	"crypto/tls"
	"encoding/hex"
	"flag"
	"io"
	"log"
	"net"
	"os"
	"os/signal"
	"runtime/pprof"
	"syscall"
	"time"

	"github.com/geph-official/geph2/libs/niaucchi3"
	"github.com/geph-official/geph2/libs/tinysocks"
	"github.com/lucas-clemente/quic-go"
)

var cookie, _ = hex.DecodeString("7b9ac53240d4eba7a22df5917e6a03fe80fcd10f3a11fff3c49f95002be12c0a")

var quicConf = &quic.Config{
	IdleTimeout:        time.Hour,
	KeepAlive:          false,
	MaxIncomingStreams: 2048,
}

var cpuprofile = flag.String("cpuprofile", "", "write cpu profile to `file`")

func main() {
	var flagClient string
	var flagServer string
	flag.StringVar(&flagClient, "c", "", "client connect")
	flag.StringVar(&flagServer, "s", "", "server listen")
	flag.Parse()
	if *cpuprofile != "" {
		f, err := os.Create(*cpuprofile)
		if err != nil {
			log.Fatal("could not create CPU profile: ", err)
		}
		c := make(chan os.Signal)
		signal.Notify(c, os.Interrupt, syscall.SIGTERM)
		if err := pprof.StartCPUProfile(f); err != nil {
			log.Fatal("could not start CPU profile: ", err)
		}
		go func() {
			<-c
			pprof.StopCPUProfile()
			f.Close()
			os.Exit(1)
		}()
	}
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
		e2e := niaucchi3.E2EListen(niaucchi3.Wrap(
			func() net.PacketConn {
				udpsock, err := net.ListenPacket("udp", "")
				if err != nil {
					panic(err)
				}
				log.Println("made new udpsock at", udpsock.LocalAddr())
				return niaucchi3.ObfsListen(cookie, udpsock)
			}))
		servuaddr, _ := net.ResolveUDPAddr("udp", flagClient)
		bremote, err := quic.Dial(e2e,
			servuaddr, "", &tls.Config{
				InsecureSkipVerify: true,
				NextProtos:         []string{"niaucchi3"},
				MinVersion:         tls.VersionTLS13,
				MaxVersion:         tls.VersionTLS13,
			}, quicConf)
		if err != nil {
			panic(err)
		}
		defer bremote.Close()
		go func() {
			for {
				client, err := listener.Accept()
				if err != nil {
					panic(err)
				}
				go func() {
					defer client.Close()
					remote, err := bremote.OpenStreamSync(context.Background())
					if err != nil {
						panic(err)
					}
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
		}()
		for {
			time.Sleep(time.Minute)
		}
	}
	// server
	if flagServer != "" {
		udpsock, err := net.ListenPacket("udp", flagServer)
		if err != nil {
			panic(err)
		}
		log.Println("server started UDP on", udpsock.LocalAddr())
		e2e := niaucchi3.E2EListen(niaucchi3.ObfsListen(cookie, udpsock))
		if err != nil {
			panic(err)
		}

		listener, err := quic.Listen(e2e, &tls.Config{
			Certificates: []tls.Certificate{genCert()},
			NextProtos:   []string{"niaucchi3"},
		}, quicConf)
		if err != nil {
			panic(err)
		}
		log.Println("QUIC socks5 listener spinned up!")
		for {
			client, err := listener.Accept(context.Background())
			if err != nil {
				panic(err)
			}
			go func() {
				defer client.Close()
				for {
					stream, err := client.AcceptStream(context.Background())
					if err != nil {
						log.Println("error accepting stream:", err)
						return
					}
					go func() {
						defer stream.Close()
						host, err := tinysocks.ReadRequest(stream)
						if err != nil {
							log.Println("error reading socks5:", err)
							return
						}
						remote, err := net.Dial("tcp", host)
						if err != nil {
							tinysocks.CompleteRequest(0x04, stream)
							return
						}
						defer remote.Close()
						tinysocks.CompleteRequest(0, stream)
						go func() {
							defer remote.Close()
							defer stream.Close()
							io.Copy(remote, stream)
						}()
						io.Copy(stream, remote)
					}()
				}
			}()
		}
	}
}
