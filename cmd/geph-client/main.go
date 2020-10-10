package main

import (
	"bufio"
	"flag"
	"fmt"
	"math/rand"
	mrand "math/rand"
	"net"
	"net/http"
	"net/http/httputil"
	"net/url"
	"os"
	"os/user"
	"runtime/debug"
	"strings"
	"time"

	"github.com/vharitonsky/iniflags"

	"github.com/acarl005/stripansi"
	"github.com/ethereum/go-ethereum/rlp"
	"github.com/geph-official/geph2/libs/bdclient"
	"golang.org/x/net/proxy"

	log "github.com/sirupsen/logrus"
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
var fakeDNS bool
var bypassChinese bool

var singleHop string
var upstreamProxy string
var additionalBridges string
var forceWarpfront bool

var sWrap *multipool

// GitVersion is the build version
var GitVersion string

var binders []*bdclient.Client

func getBindClient() *bdclient.Client {
	return binders[rand.Int()%len(binders)]
}

func getBindInfo() (string, string) {
	fronts := strings.Split(binderFront, ",")
	hosts := strings.Split(binderHost, ",")
	randIdx := rand.Int() % len(fronts)
	return fronts[randIdx], hosts[randIdx]
}

// find the fastest binder and stick to it
func binderRace() {
	fronts := strings.Split(binderFront, ",")
	hosts := strings.Split(binderHost, ",")
	if len(fronts) != len(hosts) {
		panic("binderFront and binderHost must be of identical length")
	}
	for i := 0; i < len(fronts); i++ {
		i := i
		bdc := bdclient.NewClient(fronts[i], hosts[i], fmt.Sprintf("geph_client/%v", GitVersion))
		binders = append(binders, bdc)
	}
}

func memMiser() {
	for {
		debug.FreeOSMemory()
		time.Sleep(time.Second * 5)
	}
}

func main() {
	// debug.SetGCPercent(5)
	// go memMiser()
	go mrand.Seed(time.Now().UnixNano())
	log.SetFormatter(&log.TextFormatter{
		FullTimestamp: false,
		ForceColors:   true,
	})
	log.SetLevel(log.DebugLevel)

	// configfile path
	usr, err := user.Current()
	if err != nil {
		log.Println("cannot read current user info, consider using -config=/path/to/cfgfile")
	} else {
		// default config file: $HOME/.config/client.conf, make sure chmod 600!
		// use -config=/path/to/cfgfile to override
		iniflags.SetConfigFile(usr.HomeDir + "/.config/client.conf")
		iniflags.SetAllowMissingConfigFile(true)
	}

	// flags
	flag.StringVar(&username, "username", "", "username")
	flag.StringVar(&password, "password", "", "password")
	flag.StringVar(&ticketFile, "ticketFile", "", "location for caching auth tickets")
	flag.StringVar(&binderFront, "binderFront", "https://www.netlify.com/v2", "binder domain-fronting hosts, comma separated")
	flag.StringVar(&binderHost, "binderHost", "loving-bell-981479.netlify.app", "real hostname of the binder, comma separated")
	flag.StringVar(&exitName, "exitName", "us-sfo-01.exits.geph.io", "qualified name of the exit node selected")
	flag.StringVar(&exitKey, "exitKey", "2f8571e4795032433098af285c0ce9e43c973ac3ad71bf178e4f2aaa39794aec", "ed25519 pubkey of the selected exit")
	flag.BoolVar(&forceBridges, "forceBridges", false, "force the use of obfuscated bridges")
	flag.StringVar(&socksAddr, "socksAddr", "localhost:9909", "SOCKS5 listening address")
	flag.StringVar(&httpAddr, "httpAddr", "localhost:9910", "HTTP proxy listener")
	flag.StringVar(&statsAddr, "statsAddr", "localhost:9809", "HTTP listener for statistics")
	flag.StringVar(&dnsAddr, "dnsAddr", "localhost:9983", "local DNS listener")
	flag.BoolVar(&fakeDNS, "fakeDNS", true, "return fake results for DNS")
	flag.BoolVar(&loginCheck, "loginCheck", false, "do a login check and immediately exit with code 0")
	flag.StringVar(&binderProxy, "binderProxy", "", "if set, proxy the binder at the given listening address and do nothing else")
	// flag.StringVar(&cachePath, "cachePath", os.TempDir()+"/geph-cache.db", "location of state cache")
	flag.StringVar(&upstreamProxy, "upstreamProxy", "", "upstream SOCKS5 proxy")
	flag.StringVar(&additionalBridges, "additionalBridges", "", "additional bridges, in the form of cookie1@host1;cookie2@host2 etc")
	flag.StringVar(&singleHop, "singleHop", "", "if set in form pk@host:port, location of a single-hop server. OVERRIDES BINDER AND AUTHENTICATION!")
	flag.BoolVar(&bypassChinese, "bypassChinese", false, "bypass proxy for Chinese domains")
	flag.BoolVar(&forceWarpfront, "forceWarpfront", false, "force use of warpfront")
	iniflags.Parse()
	hackDNS()
	if dnsAddr != "" {
		go doDNS()
	}
	go listenStats()
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
				sc.LogLines = append(sc.LogLines,
					stripansi.Strip(strings.TrimSpace(line)))
			})
		}
	}()
	binderRace()

	log.Println("GephNG version", GitVersion)
	// special actions
	if loginCheck {
		log.Println("loginCheck mode")
		go func() {
			time.Sleep(time.Second * 60)
			os.Exit(10)
		}()
	}
	if singleHop == "" {
		if binderProxy != "" {
			log.Println("binderProxy mode on", binderProxy)
			revProx := &httputil.ReverseProxy{
				Director: func(req *http.Request) {
					bURL, bHost := getBindInfo()
					bURLP, err := url.Parse(bURL)
					if err != nil {
						panic(err)
					}
					log.Println("reverse proxying", req.Method, req.URL)
					req.Host = bHost
					req.URL.Scheme = bURLP.Scheme
					req.URL.Host = bURLP.Host
					req.URL.Path = bURLP.Path + "/" + req.URL.Path
				},
				ModifyResponse: func(resp *http.Response) error {
					resp.Header.Add("Access-Control-Allow-Origin", "*")
					resp.Header.Add("Access-Control-Expose-Headers", "*")
					resp.Header.Add("Access-Control-Allow-Methods", "*")
					resp.Header.Add("Access-Control-Allow-Headers", "*")
					return nil
				},
			}
			if err := http.ListenAndServe(binderProxy, revProx); err != nil {
				panic(err)
			}
		}

		// connect to bridge
		// automatically pick mode
		if upstreamProxy != "" {
			log.Println("upstream proxy enabled, no bridges")
			direct = true
		} else if !forceBridges {
		retry:
			country, err := getBindClient().GetClientInfo()
			if err != nil {
				log.Println("cannot get country", err)
				goto retry
			} else {
				log.Println("country is", country.Country)
				if country.Country == "CN" {
					log.Println("in CHINA, must use bridges")
				} else {
					log.Println("disabling bridges")
					direct = true
				}
			}
		} else {
			direct = false
		}
	}
	sWrap = newMultipool()

	// confirm we are connected
	func() {
		for {
			rm, _, ok := sWrap.DialCmd("ip")
			if !ok {
				log.Println("FAILED to get IP, retrying...")
				time.Sleep(time.Second)
				continue
			}
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
			return
		}
	}()
	go listenHTTP()
	listenSocks()
}

func dialTun(dest string) (conn net.Conn, err error) {
	sks, err := proxy.SOCKS5("tcp", socksAddr, nil, proxy.Direct)
	if err != nil {
		return
	}
	conn, err = sks.Dial("tcp", dest)
	return
}
