package main

import (
	"encoding/hex"
	"errors"
	"flag"
	"io"
	"log"
	mrand "math/rand"
	"net"
	"net/http"
	"os"
	"strings"
	"time"

	"github.com/ethereum/go-ethereum/rlp"
	"github.com/geph-official/geph2/libs/bdclient"
	"github.com/geph-official/geph2/libs/cwl"
	"github.com/geph-official/geph2/libs/tinysocks"
	"github.com/xtaci/smux"
	"golang.org/x/time/rate"
)

var username string
var password string
var ticketFile string
var binderFront string
var binderHost string
var exitName string
var exitKey string
var direct bool
var loginCheck bool

var socksAddr string
var statsAddr string

var bindClient *bdclient.Client

var sWrap *muxWrap

func main() {
	mrand.Seed(time.Now().UnixNano())
	// flags
	flag.StringVar(&username, "username", "pwtest", "username")
	flag.StringVar(&password, "password", "pwtest", "password")
	flag.StringVar(&ticketFile, "ticketFile", "", "location for caching auth tickets")
	flag.StringVar(&binderFront, "binderFront", "http://binder.geph.io:9080", "front location of binder")
	flag.StringVar(&binderHost, "binderHost", "binder.geph.io", "true hostname of binder")
	flag.StringVar(&exitName, "exitName", "us-sfo-01.exits.geph.io", "qualified name of the exit node selected")
	flag.StringVar(&exitKey, "exitKey", "2f8571e4795032433098af285c0ce9e43c973ac3ad71bf178e4f2aaa39794aec", "ed25519 pubkey of the selected exit")
	flag.BoolVar(&direct, "direct", false, "bypass obfuscated bridges and directly connect")
	flag.BoolVar(&loginCheck, "loginCheck", false, "do a login check and immediately exit with code 0")
	flag.StringVar(&socksAddr, "socksAddr", "localhost:9909", "SOCKS5 listening address")
	flag.StringVar(&statsAddr, "statsAddr", "localhost:9809", "HTTP listener for statistics")
	flag.Parse()

	if loginCheck {
		go func() {
			time.Sleep(time.Second * 10)
			os.Exit(-1)
		}()
	}

	// connect to bridge
	bindClient = bdclient.NewClient(binderFront, binderHost)
	sWrap = newSmuxWrapper()

	// spin up stats server
	http.HandleFunc("/", handleStats)
	go func() {
		err := http.ListenAndServe(statsAddr, nil)
		if err != nil {
			panic(err)
		}
	}()

	// confirm we are connected
	func() {
		rm, _ := sWrap.DialCmd("ip")
		defer rm.Close()
		var ip string
		err := rlp.Decode(rm, &ip)
		if err != nil {
			log.Println("Uh oh, cannot get IP!")
			os.Exit(404)
		}
		ip = strings.TrimSpace(ip)
		log.Println("Successfully got external IP", ip)
		useStats(func(sc *stats) {
			sc.Connected = true
			sc.PublicIP = ip
		})
		if loginCheck {
			os.Exit(0)
		}
	}()

	listenLoop()
}

func listenLoop() {
	listener, err := net.Listen("tcp", socksAddr)
	if err != nil {
		panic(err)
	}
	log.Println("SOCKS5 on 9909")
	upLimit := rate.NewLimiter(100*1000, 100*1000)
	for {
		cl, err := listener.Accept()
		if err != nil {
			panic(err)
		}
		go func() {
			defer cl.Close()
			rmAddr, err := tinysocks.ReadRequest(cl)
			if err != nil {
				return
			}
			remote, ok := sWrap.DialCmd("proxy", rmAddr)
			defer remote.Close()
			log.Printf("opened %v with ok=%v", rmAddr, ok)
			if !ok {
				tinysocks.CompleteRequest(5, cl)
				return
			}
			tinysocks.CompleteRequest(0, cl)
			go func() {
				defer remote.Close()
				defer cl.Close()
				cwl.CopyWithLimit(remote, cl, upLimit, func(n int) {
					useStats(func(sc *stats) {
						sc.UpBytes += uint64(n)
					})
				})
			}()
			cwl.CopyWithLimit(cl, remote,
				rate.NewLimiter(rate.Inf, 10000000), func(n int) {
					useStats(func(sc *stats) {
						sc.DownBytes += uint64(n)
					})
				})
		}()
	}
}

func newSmuxWrapper() *muxWrap {
	return &muxWrap{getSession: func() *smux.Session {
		useStats(func(sc *stats) {
			sc.Connected = false
		})
	retry:
		// obtain a ticket
		ubmsg, ubsig, details, err := bindClient.GetTicket(username, password)
		if err != nil {
			log.Println("error authenticating:", err)
			if errors.Is(err, io.EOF) {
				os.Exit(403)
			}
			goto retry
		}
		useStats(func(sc *stats) {
			sc.Username = username
			sc.Expiry = details.PaidExpiry
			sc.Tier = details.Tier
			sc.PayTxes = details.Transactions
		})
		realExitKey, err := hex.DecodeString(exitKey)
		if err != nil {
			panic(err)
		}
		if direct {
			sm, err := getDirect([2][]byte{ubmsg, ubsig}, exitName, realExitKey)
			if err != nil {
				log.Println("direct conn retrying", err)
				time.Sleep(time.Second)
				goto retry
			}
			return sm
		}
		bridges, err := bindClient.GetBridges(ubmsg, ubsig)
		if err != nil {
			log.Println("getting bridges failed, retrying", err)
			time.Sleep(time.Second)
			goto retry
		}
		// TODO parallel
		for _, bi := range bridges {
			sm, err := getBridged([2][]byte{ubmsg, ubsig}, bi.Host, bi.Cookie, exitName, realExitKey)
			if err != nil {
				log.Println("dialing to", bi.Host, "failed!")
				continue
			}
			useStats(func(sc *stats) {
				sc.Connected = true
			})
			return sm
		}
		log.Println("everything failed, retrying")
		time.Sleep(time.Second)
		goto retry
	}}
}
