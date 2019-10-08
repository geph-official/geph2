package main

import (
	"encoding/hex"
	"flag"
	"io"
	"io/ioutil"
	"log"
	mrand "math/rand"
	"net"
	"net/http"
	"os"
	"os/signal"
	"runtime/debug"
	"runtime/pprof"
	"syscall"
	"time"

	"github.com/bunsim/goproxy"
	"github.com/geph-official/geph2/libs/kcp-go"
	"github.com/geph-official/geph2/libs/niaucchi4"
	"github.com/geph-official/geph2/libs/tinysocks"
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
	// STATS
	go func() {
		for {
			time.Sleep(time.Second * 5)
			rt := kcp.DefaultSnmp.RetransSegs
			tot := kcp.DefaultSnmp.OutPkts
			log.Printf("%2.f pct retrans, %v recovered", float64(rt)/float64(tot)*100,
				kcp.DefaultSnmp.FECRecovered)
		}
	}()
	// server
	if flagServer != "" {
		udpsock, err := net.ListenPacket("udp", flagServer)
		if err != nil {
			panic(err)
		}
		log.Println("server started UDP on", udpsock.LocalAddr())
		e2e := niaucchi4.ObfsListen(cookie, udpsock)
		if err != nil {
			panic(err)
		}

		listener, err := kcp.ServeConn(nil, 0, 0, e2e)
		if err != nil {
			panic(err)
		}
		log.Println("KCP socks5 listener spinned up!")
		for {
			kclient, err := listener.AcceptKCP()
			if err != nil {
				panic(err)
			}
			log.Println("Accepted kclient from", kclient.RemoteAddr())
			kclient.SetWindowSize(10000, 10000)
			kclient.SetNoDelay(1, 50, 3, 0)
			kclient.SetStreamMode(true)
			go func() {
				defer kclient.Close()
				client, err := yamux.Server(kclient, yamuxCfg)
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
		listener, err := net.Listen("tcp", "localhost:9909")
		if err != nil {
			panic(err)
		}
		srv := goproxy.NewProxyHttpServer()
		srv.Tr = &http.Transport{
			Dial: func(n, d string) (net.Conn, error) {
				log.Println("GONNA DIAL", n, d)
				return dialTun(d)
			},
			IdleConnTimeout: time.Second * 10,
			Proxy:           nil,
		}
		srv.Logger = log.New(ioutil.Discard, "", 0)
		go func() {
			err := http.ListenAndServe("127.0.0.1:8780", srv)
			if err != nil {
				panic(err.Error())
			}
		}()
		log.Println("TCP listener started on", listener.Addr())
		udpsock, err := net.ListenPacket("udp", "")
		if err != nil {
			panic(err)
		}
		log.Println("made new udpsock at", udpsock.LocalAddr())
		obfs := niaucchi4.ObfsListen(cookie, udpsock)
		kcpremote, err := kcp.NewConn(flagClient, nil, 0, 0, obfs)
		if err != nil {
			panic(err)
		}
		defer kcpremote.Close()
		kcpremote.SetWindowSize(10000, 10000)
		kcpremote.SetNoDelay(1, 50, 3, 0)
		kcpremote.SetStreamMode(true)
		bremote, err := yamux.Client(kcpremote, yamuxCfg)
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
