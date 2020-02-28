package main

import (
	"encoding/base64"
	"fmt"
	"io/ioutil"
	"math/rand"
	"net"
	"net/http"
	"strings"
	"sync"
	"time"

	log "github.com/sirupsen/logrus"

	"github.com/miekg/dns"
	"golang.org/x/net/proxy"
)

func doDNS() {
	log.Println("DNS on", dnsAddr)
	if fakeDNS {
		doDNSFaker()
	} else {
		doDNSProxy()
	}
}

func doDNSProxy() {
	tunProx, err := proxy.SOCKS5("tcp", socksAddr, nil, proxy.Direct)
	if err != nil {
		panic(err)
	}
	hclient := &http.Client{
		Transport: &http.Transport{
			Dial:            tunProx.Dial,
			IdleConnTimeout: time.Minute * 30,
		},
	}
	socket, err := net.ListenPacket("udp", dnsAddr)
	if err != nil {
		panic(err)
	}
	log.Println("DNS listening at", socket.LocalAddr())
	reqbuf := make([]byte, 512)
	for {
		n, addr, err := socket.ReadFrom(reqbuf)
		if err != nil {
			panic(err)
		}
		smabuf := make([]byte, n)
		copy(smabuf, reqbuf)
		go func() {
			burl := base64.RawURLEncoding.EncodeToString(smabuf)
			rsp, err := hclient.Get("https://cloudflare-dns.com/dns-query?dns=" + burl)
			if err != nil {
				return
			}
			response, err := ioutil.ReadAll(rsp.Body)
			if err != nil {
				log.Println("error from Cloudflare reader:", err)
				return
			}
			socket.WriteTo(response, addr)
		}()
	}
}

func doDNSFaker() {
	// our server
	serv := &dns.Server{
		Net:  "udp",
		Addr: dnsAddr,
		Handler: dns.HandlerFunc(func(w dns.ResponseWriter, r *dns.Msg) {
			q := r.Question[0]
			// we can't do anything if not A or CNAME
			if q.Qtype == dns.TypeA || q.Qtype == dns.TypeCNAME {
				ans := nameToFakeIP(q.Name)
				ip, _ := net.ResolveIPAddr("ip4", ans)
				m := new(dns.Msg)
				m.SetReply(r)
				m.Answer = append(m.Answer, &dns.A{
					Hdr: dns.RR_Header{
						Name:   q.Name,
						Rrtype: dns.TypeA,
						Class:  dns.ClassINET,
						Ttl:    1,
					},
					A: ip.IP,
				})
				w.WriteMsg(m)
			}
			dns.HandleFailed(w, r)
		}),
	}
	err := serv.ListenAndServe()
	if err != nil {
		log.Println("Uh oh!")
		panic(err.Error())
	}
}

var fakeIPCache struct {
	mapping map[string]string
	revmap  map[string]string
	lock    sync.Mutex
}

func init() {
	fakeIPCache.mapping = make(map[string]string)
	fakeIPCache.revmap = make(map[string]string)
}

func nameToFakeIP(name string) string {
	fakeIPCache.lock.Lock()
	defer fakeIPCache.lock.Unlock()
	if fakeIPCache.mapping[name] != "" {
		return fakeIPCache.mapping[name]
	}
	genFakeIP := func() string {
		return fmt.Sprintf("100.%v.%v.%v", rand.Int()%64+64, rand.Int()%256, rand.Int()%256)
	}
	// find an unallocated name
	retval := genFakeIP()
	for fakeIPCache.revmap[retval] != "" {
		retval = genFakeIP()
	}
	log.Debugf("mapped fake IP %v => %v", retval, name)
	fakeIPCache.revmap[retval] = strings.Trim(name, ".")
	fakeIPCache.mapping[name] = retval
	return retval
}

func fakeIPToName(ip string) string {
	fakeIPCache.lock.Lock()
	defer fakeIPCache.lock.Unlock()
	return fakeIPCache.revmap[ip]
}
