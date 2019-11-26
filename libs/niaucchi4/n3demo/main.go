package main

import (
	"encoding/hex"
	"flag"
	"io"
	"log"
	mrand "math/rand"
	"net"
	"os"
	"os/signal"
	"runtime/debug"
	"runtime/pprof"
	"syscall"
	"time"

	"github.com/geph-official/geph2/libs/kcp-go"
	"github.com/xtaci/smux"
)

var cookie, _ = hex.DecodeString("7b9ac53240d4eba7a22df5917e6a03fe80fcd10f3a11fff3c49f95002be12c0a")

var cpuprofile = flag.String("cpuprofile", "", "write cpu profile to `file`")

var yamuxCfg = &smux.Config{
	KeepAliveInterval: time.Minute,
	KeepAliveTimeout:  time.Minute * 20,
	MaxFrameSize:      16384,
	MaxReceiveBuffer:  16384 * 1000,
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
		listener, err := kcp.ServeConn(nil, 0, 0, udpsock)
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
			kclient.SetWindowSize(100000, 100000)
			kclient.SetNoDelay(0, 100, 3, 0)
			kclient.SetStreamMode(true)
			go func() {
				defer kclient.Close()
				client, err := smux.Server(kclient, yamuxCfg)
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
						for {
							stream.Write(make([]byte, 1048576))
						}
					}()
				}
			}()
		}
	}
	// client
	if flagClient != "" {
		listener, err := net.Listen("tcp", "localhost:9999")
		if err != nil {
			panic(err)
		}
		log.Println("TCP listener started on", listener.Addr())
		udpsock, err := net.ListenPacket("udp", "")
		if err != nil {
			panic(err)
		}
		log.Println("made new udpsock at", udpsock.LocalAddr())
		kcpremote, err := kcp.NewConn(flagClient, nil, 0, 0, udpsock)
		if err != nil {
			panic(err)
		}
		defer kcpremote.Close()
		kcpremote.SetWindowSize(10000, 10000)
		kcpremote.SetNoDelay(0, 100, 3, 0)
		kcpremote.SetStreamMode(true)
		bremote, err := smux.Client(kcpremote, yamuxCfg)
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
