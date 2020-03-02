package niaucchi4

import (
	"fmt"
	"net"
	"testing"
	"time"

	"github.com/geph-official/geph2/libs/tinysocks"
	log "github.com/sirupsen/logrus"
)

func TestProxy(t *testing.T) {
	//server
	serverlisten := "192.168.1.6:10001"
	server, err := net.ListenPacket("udp", serverlisten)
	if err != nil {
		panic(err)
	}
	defer server.Close()
	go func() {
		p := make([]byte, 1000)
		for {
			n, addr, err := server.ReadFrom(p)
			if err != nil {
				panic(err)
			}
			log.Println("SERVER get lenth", len(string(p[:n])), string(p[:n]), "from", addr)
			server.WriteTo(p[:n], addr)
		}
	}()

	//client
	singleHopProxy := "localhost:332"
	proxytcp, err := net.Dial("tcp", singleHopProxy)
	if err != nil {
		log.Warnln("failed to connect to singlehop server:", err)
		return
	}
	var udpadd tinysocks.Addr
	err, udpadd = tinysocks.Client(proxytcp, tinysocks.ParseAddr(serverlisten), tinysocks.CmdUDPAssociate)
	if err != nil {
		log.Warnln("failed to shakehand with second socks5 server:", err)
		return
	}
	log.Println("UDP port", udpadd.String())
	go func() {
		buf := make([]byte, 1)
		// block here
		for {
			_, err := proxytcp.Read(buf)
			if err, ok := err.(net.Error); ok && err.Timeout() {
				continue
			}
			log.Debugln("UDP Associate End.")
			return
		}
	}()
	client, err := net.ListenPacket("udp", "192.168.1.6:10002")
	if err != nil {
		panic(err)
	}
	defer client.Close()

	client = NewProxyConn(client, udpadd.String())

	go func() {
		p := make([]byte, 1000)
		for {
			n, addr, err := client.ReadFrom(p)
			if err != nil {
				panic(err)
			}
			log.Println("CLIENT get lenth", len(string(p[:n])), string(p[:n]), "from", addr)
		}
	}()

	o1addr, err := net.ResolveUDPAddr("udp", serverlisten)
	if err != nil {
		panic(err)
	}
	for i := 0; ; i++ {
		datatosend := fmt.Sprintf("MSG-%v", i)
		log.Println("CLIENT start to send len", len(datatosend), datatosend, "to", o1addr.String())
		client.WriteTo([]byte(datatosend), o1addr)
		time.Sleep(time.Second * 5)
	}
}
