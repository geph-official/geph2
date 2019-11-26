package fastudp

import (
	"net"
	"testing"
)

func BenchmarkStockUDP(b *testing.B) {
	pc, err := net.ListenPacket("udp", ":")
	if err != nil {
		panic(err)
	}
	defer pc.Close()
	tgtAddr, _ := net.ResolveUDPAddr("udp", "localhost:11111")
	for i := 0; i < b.N; i++ {
		_, err = pc.WriteTo(make([]byte, 1024), tgtAddr)
		if err != nil {
			panic(err)
		}
	}
}
