package main

import (
	"crypto/ed25519"
	"flag"
	"io"
	"io/ioutil"
	"log"
	"net"
	"net/http"
	"time"

	"github.com/ethereum/go-ethereum/rlp"
	"github.com/geph-official/geph2/libs/bdclient"
	"github.com/geph-official/geph2/libs/cwl"
	"github.com/geph-official/geph2/libs/tinyss"
	"github.com/patrickmn/go-cache"
	"github.com/xtaci/smux"
	"golang.org/x/time/rate"
)

var keyfile string
var pubkey ed25519.PublicKey
var seckey ed25519.PrivateKey

var binderFront string
var binderReal string
var bclient *bdclient.Client

var ipcache = cache.New(time.Hour, time.Hour)

func main() {
	flag.StringVar(&keyfile, "keyfile", "keyfile.bin", "location of key file")
	flag.StringVar(&binderFront, "binderFront", "http://binder.geph.io:9080", "binder domain-fronting host")
	flag.StringVar(&binderReal, "binderReal", "binder.geph.io", "real hostname of the binder")
	flag.Parse()
	bclient = bdclient.NewClient(binderFront, binderReal)
	// load the key
	loadKey()
	log.Printf("Loaded PK = %x", pubkey)
	// listen
	tcpListener, err := net.Listen("tcp", ":2389")
	if err != nil {
		panic(err)
	}
	log.Println("Started MUX on TinySS on port 2389")
	for {
		rawClient, err := tcpListener.Accept()
		if err != nil {
			panic(err)
		}
		go func() {
			defer rawClient.Close()
			rawClient.SetDeadline(time.Now().Add(time.Second * 30))
			tssClient, err := tinyss.Handshake(rawClient)
			if err != nil {
				log.Println("Error doing TinySS from", rawClient.RemoteAddr(), err)
				return
			}
			defer tssClient.Close()
			// sign the shared secret
			ssSignature := ed25519.Sign(seckey, tssClient.SharedSec())
			rlp.Encode(tssClient, &ssSignature)
			rawClient.SetDeadline(time.Time{})
			// create smux context
			muxSrv, err := smux.Server(tssClient, &smux.Config{
				KeepAliveInterval: time.Minute * 30,
				KeepAliveTimeout:  time.Minute * 32,
				MaxFrameSize:      10000,
				MaxReceiveBuffer:  1024 * 1024 * 100,
			})
			if err != nil {
				log.Println("Error negotiating smux from", rawClient.RemoteAddr(), err)
				return
			}
			// authenticate the client
			var greeting [2][]byte
			err = rlp.Decode(tssClient, &greeting)
			if err != nil {
				log.Println("Error decoding greeting from", rawClient.RemoteAddr(), err)
				return
			}
			var limiter *rate.Limiter
			err = bclient.RedeemTicket("paid", greeting[0], greeting[1])
			if err != nil {
				log.Printf("%v isn't paid, trying free", rawClient.RemoteAddr())
				err = bclient.RedeemTicket("free", greeting[0], greeting[1])
				if err != nil {
					log.Printf("%v isn't free either. fail", rawClient.RemoteAddr())
					rlp.Encode(tssClient, "FAIL")
					return
				}
				log.Printf("logging in %v as a free user with 800 Kbps", rawClient.RemoteAddr())
				limiter = rate.NewLimiter(100*1000, 1000*1000)
			} else {
				log.Printf("logging in %v as a paid user", rawClient.RemoteAddr())
				limiter = rate.NewLimiter(rate.Inf, 10000*1000)
			}
			// IGNORE FOR NOW
			rlp.Encode(tssClient, "OK")
			defer muxSrv.Close()
			for {
				soxclient, err := muxSrv.AcceptStream()
				if err != nil {
					return
				}
				go func() {
					defer soxclient.Close()
					var command []string
					err = rlp.Decode(&io.LimitedReader{R: soxclient, N: 1000}, &command)
					if err != nil {
						return
					}
					if len(command) == 0 {
						return
					}
					// match command
					switch command[0] {
					case "proxy":
						if len(command) < 1 {
							return
						}
						host := command[1]
						remote, err := net.DialTimeout("tcp", host, time.Second*2)
						if err != nil {
							rlp.Encode(soxclient, false)
							return
						}
						log.Println("dialed to", host)
						remote.SetDeadline(time.Now().Add(time.Hour))
						rlp.Encode(soxclient, true)
						defer remote.Close()
						go func() {
							defer remote.Close()
							defer soxclient.Close()
							cwl.CopyWithLimit(remote, soxclient, limiter, nil)
						}()
						cwl.CopyWithLimit(soxclient, remote, limiter, nil)
					case "ip":
						var ip string
						if ipi, ok := ipcache.Get("ip"); ok {
							ip = ipi.(string)
						} else {
							addr := "http://checkip.amazonaws.com"
							resp, err := http.Get(addr)
							if err != nil {
								return
							}
							defer resp.Body.Close()
							ipb, err := ioutil.ReadAll(resp.Body)
							if err != nil {
								return
							}
							ip = string(ipb)
						}
						rlp.Encode(soxclient, true)
						rlp.Encode(soxclient, ip)
					}
				}()
			}
		}()
	}
}

func loadKey() {
retry:
	bts, err := ioutil.ReadFile(keyfile)
	if err != nil {
		// genkey
		_, key, _ := ed25519.GenerateKey(nil)
		ioutil.WriteFile(keyfile, key, 0600)
		goto retry
	}
	seckey = bts
	pubkey = seckey.Public().(ed25519.PublicKey)
}
