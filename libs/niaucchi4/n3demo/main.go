package main

import (
	"encoding/hex"
	"flag"
	"io"
	"io/ioutil"
	"log"
	mrand "math/rand"
	"net"
	"os"
	"os/signal"
	"runtime/debug"
	"runtime/pprof"
	"syscall"
	"time"

	"github.com/geph-official/geph2/libs/niaucchi4/backedtcp"
	"github.com/geph-official/geph2/libs/tinysocks"
	"github.com/geph-official/geph2/libs/tinyss"
	"github.com/hashicorp/yamux"
	"golang.org/x/net/proxy"
)

var cookie, _ = hex.DecodeString("7b9ac53240d4eba7a22df5917e6a03fe80fcd10f3a11fff3c49f95002be12c0a")

var cpuprofile = flag.String("cpuprofile", "", "write cpu profile to `file`")

var yamuxCfg = &yamux.Config{
	AcceptBacklog:          100,
	EnableKeepAlive:        false,
	KeepAliveInterval:      time.Minute,
	ConnectionWriteTimeout: time.Hour,
	MaxStreamWindowSize:    16384 * 1400,
	LogOutput:              ioutil.Discard,
}

func dialTun(dest string) (conn net.Conn, err error) {
	sks, err := proxy.SOCKS5("tcp", "127.0.0.1:9909", nil, proxy.Direct)
	if err != nil {
		return
	}
	conn, err = sks.Dial("tcp", dest)
	return
}

func main() {
	var flagClient string
	var flagServer string
	flag.StringVar(&flagClient, "c", "", "client connect")
	flag.StringVar(&flagServer, "s", "", "server listen")
	flag.Parse()
	mrand.Seed(time.Now().UnixNano())
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
			debug.SetTraceback("all")
			panic("TB")
		}()
	}
	if flagClient == "" && flagServer == "" {
		log.Fatal("must give -c or -s")
	}
	if flagClient != "" && flagServer != "" {
		log.Fatal("cannot give both -c or -s")
	}
	// server
	if flagServer != "" {
		tlistener, err := net.Listen("tcp", flagServer)
		if err != nil {
			panic(err)
		}
		listener := backedtcp.Listen(tlistener)
		log.Println("KCP socks5 listener spinned up!")
		for {
			kclient, err := listener.Accept()
			if err != nil {
				panic(err)
			}
			log.Println("Accepted kclient from", kclient.RemoteAddr())
			go func() {
				defer kclient.Close()
				cryptClient, _ := tinyss.Handshake(kclient)
				client, err := yamux.Server(cryptClient, yamuxCfg)
				if err != nil {
					log.Println("cannot start yamux:", err)
					return
				}
				defer client.Close()
				for {
					stream, err := client.AcceptStream()
					if err != nil {
						log.Println("error accepting stream:", err)
						return
					}
					log.Println("accepted stream")
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
	// client
	if flagClient != "" {
		listener, err := net.Listen("tcp", "localhost:8808")
		if err != nil {
			panic(err)
		}
		log.Println("TCP listener started on", listener.Addr())
		zremote, err := net.Dial("tcp", flagClient)
		if err != nil {
			panic(err)
		}
		defer zremote.Close()
		zremote.Write(make([]byte, 8))
		remote := backedtcp.NewSocket(zremote)
		go func() {
			for {
				time.Sleep(time.Second * 10)
				zremote, err := net.Dial("tcp", flagClient)
				if err != nil {
					log.Println(err)
					continue
				}
				defer zremote.Close()
				zremote.Write(make([]byte, 8))
				remote.Replace(zremote)
			}
		}()
		cryptRemote, _ := tinyss.Handshake(remote)
		bremote, err := yamux.Client(cryptRemote, yamuxCfg)
		if err != nil {
			panic(err)
		}
		go func() {
			for {
				client, err := listener.Accept()
				if err != nil {
					panic(err)
				}
				go func() {
					defer client.Close()
					remote, err := bremote.OpenStream()
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

}
