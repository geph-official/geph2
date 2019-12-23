package main

import (
	"bufio"
	"flag"
	"fmt"
	"io/ioutil"
	"log"
	mrand "math/rand"
	"net"
	"net/http"
	"net/http/httputil"
	"net/url"
	"os"
	"os/signal"
	"runtime"
	"runtime/debug"
	"runtime/pprof"
	"strings"
	"syscall"
	"time"

	"github.com/bunsim/goproxy"
	"github.com/ethereum/go-ethereum/rlp"
	"github.com/geph-official/geph2/libs/bdclient"
	"github.com/geph-official/geph2/libs/kcp-go"
	"golang.org/x/net/proxy"
)

var username string
var password string
var ticketFile string
var binderFront string
var binderHost string
var exitName string
var exitKey string
var forceBridges bool

var loginCheck bool
var binderProxy string

var direct bool

var socksAddr string
var httpAddr string
var statsAddr string
var dnsAddr string
var cachePath string

var useTCP bool

var bindClient *bdclient.Client

var cpuprofile = flag.String("cpuprofile", "", "write cpu profile to `file`")

var sWrap *muxWrap

// GitVersion is the build version
var GitVersion string

// find the fastest binder and stick to it
func binderRace() {
	log.Println("starting binder race...")
restart:
	fronts := strings.Split(binderFront, ",")
	hosts := strings.Split(binderHost, ",")
	if len(fronts) != len(hosts) {
		panic("binderFront and binderHost must be of identical length")
	}
	winner := make(chan int, 1000)
	for i := 0; i < len(fronts); i++ {
		i := i
		bdc := bdclient.NewClient(fronts[i], hosts[i])
		go func() {
			_, err := bdc.GetClientInfo()
			if err != nil {
				log.Printf("[%v %v] failed: %e", fronts[i], hosts[i], err)
				return
			}
			winner <- i
		}()
	}
	select {
	case i := <-winner:
		log.Printf("[%v %v] won binder race", fronts[i], hosts[i])
		binderFront = fronts[i]
		binderHost = hosts[i]
	case <-time.After(time.Second * 20):
		goto restart
	}
}

func main() {
	mrand.Seed(time.Now().UnixNano())
	runtime.GOMAXPROCS(1)
	kcp.CongestionControl = "BIC"
	// flags
	flag.StringVar(&username, "username", "", "username")
	flag.StringVar(&password, "password", "", "password")
	flag.StringVar(&ticketFile, "ticketFile", "", "location for caching auth tickets")
	flag.StringVar(&binderFront, "binderFront", "https://www.cdn77.com/v2,https://netlify.com/front/v2,https://ajax.aspnetcdn.com/v2", "binder domain-fronting hosts, comma separated")
	flag.StringVar(&binderHost, "binderHost", "1680337695.rsc.cdn77.org,gracious-payne-f3e2ed.netlify.com,gephbinder.azureedge.net", "real hostname of the binder, comma separated")
	flag.StringVar(&exitName, "exitName", "us-sfo-01.exits.geph.io", "qualified name of the exit node selected")
	flag.StringVar(&exitKey, "exitKey", "2f8571e4795032433098af285c0ce9e43c973ac3ad71bf178e4f2aaa39794aec", "ed25519 pubkey of the selected exit")
	flag.BoolVar(&forceBridges, "forceBridges", false, "force the use of obfuscated bridges")
	flag.StringVar(&socksAddr, "socksAddr", "localhost:9909", "SOCKS5 listening address")
	flag.StringVar(&httpAddr, "httpAddr", "localhost:9910", "HTTP proxy listener")
	flag.StringVar(&statsAddr, "statsAddr", "localhost:9809", "HTTP listener for statistics")
	flag.StringVar(&dnsAddr, "dnsAddr", "localhost:9983", "local DNS listener")
	flag.BoolVar(&loginCheck, "loginCheck", false, "do a login check and immediately exit with code 0")
	flag.StringVar(&binderProxy, "binderProxy", "", "if set, proxy the binder at the given listening address and do nothing else")
	flag.StringVar(&cachePath, "cachePath", os.TempDir()+"geph-cache.db", "location of state cache")
	flag.BoolVar(&useTCP, "useExperimentalTCP", false, "use TCP to connect to bridges")
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
			debug.SetTraceback("all")
			panic("TB")
		}()
	}
	if GitVersion == "" {
		GitVersion = "NOVER"
	}
	logPipeR, logPipeW, _ := os.Pipe()
	log.SetOutput(logPipeW)
	go func() {
		buffi := bufio.NewReader(logPipeR)
		for {
			line, err := buffi.ReadString('\n')
			if err != nil {
				return
			}
			fmt.Fprint(os.Stderr, line)
			useStats(func(sc *stats) {
				sc.LogLines = append(sc.LogLines, strings.TrimSpace(line))
			})
		}
	}()

	log.Println("GephNG version", GitVersion)
	// special actions
	if loginCheck {
		log.Println("loginCheck mode")
		go func() {
			time.Sleep(time.Second * 60)
			os.Exit(10)
		}()
	}
	binderRace()
	if binderProxy != "" {
		log.Println("binderProxy mode on", binderProxy)
		binderURL, err := url.Parse(binderFront)
		if err != nil {
			panic(err)
		}
		revProx := &httputil.ReverseProxy{
			Director: func(req *http.Request) {
				log.Println("reverse proxying", req.Method, req.URL)
				req.Host = binderHost
				req.URL.Scheme = binderURL.Scheme
				req.URL.Host = binderURL.Host
				req.URL.Path = binderURL.Path + "/" + req.URL.Path
			},
			ModifyResponse: func(resp *http.Response) error {
				resp.Header.Add("Access-Control-Allow-Origin", "*")
				resp.Header.Add("Access-Control-Expose-Headers", "*")
				resp.Header.Add("Access-Control-Allow-Methods", "*")
				resp.Header.Add("Access-Control-Allow-Headers", "*")
				return nil
			},
		}
		if http.ListenAndServe(binderProxy, revProx) != nil {
			panic(err)
		}
	}

	// connect to bridge
	bindClient = bdclient.NewClient(binderFront, binderHost)
	sWrap = newSmuxWrapper()

	// automatically pick mode
	if !forceBridges {
		country, err := bindClient.GetClientInfo()
		if err != nil {
			log.Println("cannot get country, conservatively using bridges", err)
		} else {
			log.Println("country is", country.Country)
			if country.Country == "CN" || country.Country == "" {
				log.Println("in CHINA, must use bridges")
			} else {
				log.Println("disabling bridges")
				direct = true
			}
		}
	} else {
		direct = false
	}

	if dnsAddr != "" {
		go doDNSProxy()
	}
	go listenStats()

	// confirm we are connected
	func() {
		for {
			rm, _ := sWrap.DialCmd("ip")
			defer rm.Close()
			var ip string
			err := rlp.Decode(rm, &ip)
			if err != nil {
				log.Println("Uh oh, cannot get IP!", err)
				continue
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
			return
		}
	}()

	// HTTP proxy
	srv := goproxy.NewProxyHttpServer()
	srv.Tr = &http.Transport{
		Dial: func(n, d string) (net.Conn, error) {
			return dialTun(d)
		},
		IdleConnTimeout: time.Second * 60,
		Proxy:           nil,
	}
	srv.Logger = log.New(ioutil.Discard, "", 0)
	go func() {
		proxServ := &http.Server{
			Addr:        httpAddr,
			Handler:     srv,
			ReadTimeout: time.Minute * 5,
			IdleTimeout: time.Minute * 5,
		}
		err := proxServ.ListenAndServe()
		if err != nil {
			panic(err.Error())
		}
	}()

	listenSocks()
}

func dialTun(dest string) (conn net.Conn, err error) {
	sks, err := proxy.SOCKS5("tcp", "127.0.0.1:9909", nil, proxy.Direct)
	if err != nil {
		return
	}
	conn, err = sks.Dial("tcp", dest)
	return
}
