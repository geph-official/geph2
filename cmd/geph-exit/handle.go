package main

import (
	"context"
	"crypto/ed25519"
	"fmt"
	"io"
	"io/ioutil"
	"net"
	"net/http"
	"sync/atomic"
	"time"

	log "github.com/sirupsen/logrus"

	"github.com/ethereum/go-ethereum/rlp"
	"github.com/geph-official/geph2/libs/cwl"
	"github.com/geph-official/geph2/libs/tinyss"
	"github.com/hashicorp/yamux"
	"github.com/xtaci/smux"
	"golang.org/x/time/rate"
)

// blacklist of local networks
var cidrBlacklist []*net.IPNet

func init() {
	for _, s := range []string{
		"127.0.0.1/8",
		"10.0.0.0/8",
		"172.16.0.0/12",
		"192.168.0.0/16",
		"::1/128",
	} {
		_, n, _ := net.ParseCIDR(s)
		cidrBlacklist = append(cidrBlacklist, n)
	}
}

func isBlack(addr *net.TCPAddr) bool {
	for _, n := range cidrBlacklist {
		if n.Contains(addr.IP) {
			return true
		}
	}
	return false
}

var sessCount uint64

func init() {
	go func() {
		for {
			time.Sleep(time.Second * 10)
			if statClient != nil {
				statClient.Send(map[string]string{
					hostname + ".sessionCount": fmt.Sprintf("%v|g", atomic.LoadUint64(&sessCount)),
				}, 1)
			}
		}
	}()
}

func handle(rawClient net.Conn) {
	log.Debugf("[%v] accept", rawClient.RemoteAddr())
	defer log.Debugf("[%v] close", rawClient.RemoteAddr())
	defer rawClient.Close()
	rawClient.SetDeadline(time.Now().Add(time.Second * 30))
	tssClient, err := tinyss.Handshake(rawClient, 0)
	if err != nil {
		log.Println("Error doing TinySS from", rawClient.RemoteAddr(), err)
		return
	}
	defer tssClient.Close()
	atomic.AddUint64(&sessCount, 1)
	defer atomic.AddUint64(&sessCount, ^uint64(0))
	// copy the streams while
	var counter uint64
	// HACK: it's bridged if the remote address has a dot in it
	//isBridged := strings.Contains(rawClient.RemoteAddr().String(), ".")
	// sign the shared secret
	ssSignature := ed25519.Sign(seckey, tssClient.SharedSec())
	rlp.Encode(tssClient, &ssSignature)
	var limiter *rate.Limiter
	limiter = rate.NewLimiter(rate.Limit(speedLimit*1024), speedLimit*1024*10)
	// "generic" stuff
	var acceptStream func() (net.Conn, error)
	if singleHop == "" {
		// authenticate the client
		var greeting [2][]byte
		err = rlp.Decode(tssClient, &greeting)
		if err != nil {
			log.Println("Error decoding greeting from", rawClient.RemoteAddr(), err)
			return
		}
		err = bclient.RedeemTicket("paid", greeting[0], greeting[1])
		if err != nil {
			if onlyPaid {
				log.Printf("%v isn't paid and we only accept paid. Failing!", rawClient.RemoteAddr())
				rlp.Encode(tssClient, "FAIL")
				return
			}
			err = bclient.RedeemTicket("free", greeting[0], greeting[1])
			if err != nil {
				log.Printf("%v isn't free either. fail", rawClient.RemoteAddr())
				rlp.Encode(tssClient, "FAIL")
				return
			}
			limiter = rate.NewLimiter(100*1000, 1*1000*1000)
			limiter.WaitN(context.Background(), 1*1000*1000-500)
		}
		// IGNORE FOR NOW
		rlp.Encode(tssClient, "OK")
	}
	switch tssClient.NextProt() {
	case 0:
		// create smux context
		muxSrv, err := smux.Server(tssClient, &smux.Config{
			Version:           1,
			KeepAliveInterval: time.Minute * 10,
			KeepAliveTimeout:  time.Minute * 40,
			MaxFrameSize:      8192,
			MaxReceiveBuffer:  100 * 1024 * 1024,
			MaxStreamBuffer:   10 * 1024 * 1024,
		})
		if err != nil {
			log.Println("Error negotiating smux from", rawClient.RemoteAddr(), err)
			return
		}
		acceptStream = func() (n net.Conn, e error) {
			n, e = muxSrv.AcceptStream()
			return
		}
	case 2:
		// create smux context
		muxSrv, err := smux.Server(tssClient, &smux.Config{
			Version:           2,
			KeepAliveInterval: time.Minute * 10,
			KeepAliveTimeout:  time.Minute * 40,
			MaxFrameSize:      32768,
			MaxReceiveBuffer:  100 * 1024 * 1024,
			MaxStreamBuffer:   100 * 1024 * 1024,
		})
		if err != nil {
			log.Println("Error negotiating smux from", rawClient.RemoteAddr(), err)
			return
		}
		acceptStream = func() (n net.Conn, e error) {
			n, e = muxSrv.AcceptStream()
			return
		}
	case 'S':
		// create smux context
		muxSrv, err := yamux.Server(tssClient, &yamux.Config{
			AcceptBacklog:          1000,
			EnableKeepAlive:        false,
			KeepAliveInterval:      time.Hour,
			ConnectionWriteTimeout: time.Minute * 30,
			MaxStreamWindowSize:    100 * 1024 * 1024,
			LogOutput:              ioutil.Discard,
		})
		if err != nil {
			log.Println("Error negotiating yamux from", rawClient.RemoteAddr(), err)
			return
		}
		acceptStream = func() (n net.Conn, e error) {
			n, e = muxSrv.AcceptStream()
			return
		}
	}
	rawClient.SetDeadline(time.Now().Add(time.Hour * 24))
	for {
		soxclient, err := acceptStream()
		if err != nil {
			return
		}
		go func() {
			defer soxclient.Close()
			soxclient.SetDeadline(time.Now().Add(time.Minute))
			var command []string
			err = rlp.Decode(&io.LimitedReader{R: soxclient, N: 1000}, &command)
			if err != nil {
				return
			}
			if len(command) == 0 {
				return
			}
			soxclient.SetDeadline(time.Time{})
			log.Debugf("[%v] cmd %v", rawClient.RemoteAddr(), command)
			// match command
			switch command[0] {
			case "proxy":
				if len(command) < 1 {
					return
				}
				rlp.Encode(soxclient, true)
				dialStart := time.Now()
				host := command[1]
				var remote net.Conn
				for _, ntype := range []string{"tcp6", "tcp4"} {
					tcpAddr, err := net.ResolveTCPAddr(ntype, host)
					if err != nil || isBlack(tcpAddr) {
						continue
					}
					remote, err = net.DialTimeout("tcp", tcpAddr.String(), time.Second*30)
					if err != nil {
						continue
					}
					break
				}
				if remote == nil {
					return
				}
				// measure dial latency
				dialLatency := time.Since(dialStart)
				if statClient != nil && singleHop == "" {
					statClient.Timing(hostname+".dialLatency", dialLatency.Milliseconds())
					statClient.Increment(hostname + ".totalConns")
					defer func() {
						statClient.Timing(hostname+".connLifetime", dialLatency.Milliseconds())
					}()
				}

				remote.SetDeadline(time.Now().Add(time.Hour))
				defer remote.Close()
				onPacket := func(l int) {
					if statClient != nil && singleHop == "" {
						before := atomic.LoadUint64(&counter)
						atomic.AddUint64(&counter, uint64(l))
						after := atomic.LoadUint64(&counter)
						if before/1000000 != after/1000000 {
							statClient.Increment(hostname + ".transferMB")
						}
					}
				}
				go func() {
					defer remote.Close()
					defer soxclient.Close()
					cwl.CopyWithLimit(remote, soxclient, limiter, onPacket)
				}()
				cwl.CopyWithLimit(soxclient, remote, limiter, onPacket)
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
					ipcache.SetDefault("ip", ip)
				}
				rlp.Encode(soxclient, true)
				rlp.Encode(soxclient, ip)
				time.Sleep(time.Second)
			}
		}()
	}
}
