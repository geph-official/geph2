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
	tgtAddr, _ := net.ResolveUDPAddr("udp", "1.1.1.1:11111")
	batchPC := NewConn(pc.(*net.UDPConn))
	zz := []byte("HELLO")
	for i := 0; i < b.N; i++ {
		_, err = batchPC.WriteTo(zz, tgtAddr)
		if err != nil {
			panic(err)
		}
	}
}
