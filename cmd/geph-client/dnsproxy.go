package main

import (
	"encoding/base64"
	"io/ioutil"
	"net"
	"net/http"
	"time"

	log "github.com/sirupsen/logrus"

	"golang.org/x/net/proxy"
)

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
	log.Println("DNS proxy listening at", socket.LocalAddr())
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
