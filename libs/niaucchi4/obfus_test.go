package niaucchi4

import (
	"crypto/rand"
	"fmt"
	"log"
	"net"
	"testing"
	"time"
)

func TestObfs(t *testing.T) {
	sock1, err := net.ListenPacket("udp", "127.0.0.1:10001")
	if err != nil {
		panic(err)
	}
	defer sock1.Close()
	sock2, err := net.ListenPacket("udp", "127.0.0.1:10002")
	if err != nil {
		panic(err)
	}
	defer sock2.Close()
	cookie := make([]byte, 32)
	rand.Read(cookie)
	// make obfs
	obfs1 := ObfsListen(cookie, sock1)
	obfs2 := ObfsListen(cookie, sock2)
	// test
	go func() {
		p := make([]byte, 1000)
		for {
			n, addr, err := obfs1.ReadFrom(p)
			if err != nil {
				panic(err)
			}
			log.Println("replying to ping from", addr)
			obfs1.WriteTo(p[:n], addr)
		}
	}()
	go func() {
		p := make([]byte, 1000)
		for {
			n, addr, err := obfs2.ReadFrom(p)
			if err != nil {
				panic(err)
			}
			log.Println("RECV from", addr, "msg", string(p[:n]))
		}
	}()
	o1addr, err := net.ResolveUDPAddr("udp", "127.0.0.1:10001")
	if err != nil {
		panic(err)
	}
	for i := 0; ; i++ {
		obfs2.WriteTo([]byte(fmt.Sprintf("MSG-%v", i)), o1addr)
		time.Sleep(time.Second)
	}
}
