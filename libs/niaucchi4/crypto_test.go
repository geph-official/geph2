package niaucchi4

import (
	"crypto/rand"
	"testing"
)

func BenchmarkRandNonce(b *testing.B) {
	for i := 0; i < b.N; i++ {
		rand.Read(make([]byte, 16))
	}
}

func BenchmarkTunStateEncDec(b *testing.B) {
	ts1 := &tunstate{isserv: true}
	ts2 := &tunstate{isserv: false}
	ts1.deriveKeys(make([]byte, 32))
	ts2.deriveKeys(make([]byte, 32))
	for i := 0; i < b.N; i++ {
		ct := ts1.Encrypt(make([]byte, 1024))
		_, err := ts2.Decrypt(ct)
		if err != nil {
			panic(err)
		}
	}
}
