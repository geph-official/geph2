[1mdiff --git a/libs/tinyss/socket.go b/libs/tinyss/socket.go[m
[1mindex ba1363a..7100533 100644[m
[1m--- a/libs/tinyss/socket.go[m
[1m+++ b/libs/tinyss/socket.go[m
[36m@@ -12,6 +12,7 @@[m [mimport ([m
 	"time"[m
 [m
 	"github.com/geph-official/geph2/libs/c25519"[m
[32m+[m	[32mpool "github.com/libp2p/go-buffer-pool"[m
 	"golang.org/x/crypto/chacha20poly1305"[m
 	"golang.org/x/crypto/curve25519"[m
 )[m
[36m@@ -103,17 +104,19 @@[m [mfunc (sk *Socket) Read(p []byte) (n int, err error) {[m
 		sk.rxerr = err[m
 		return[m
 	}[m
[31m-	ciph := make([]byte, binary.BigEndian.Uint16(lenbts))[m
[32m+[m	[32mciph := pool.GlobalPool.Get(int(binary.BigEndian.Uint16(lenbts)))[m
[32m+[m	[32mdefer pool.GlobalPool.Put(ciph)[m
 	_, err = io.ReadFull(sk.plainBuffered, ciph)[m
 	if err != nil {[m
 		sk.rxerr = err[m
 		return[m
 	}[m
 	// decrypt the ciphertext[m
[31m-	nonce := make([]byte, sk.rxcrypt.NonceSize())[m
[32m+[m	[32mnonce := pool.GlobalPool.Get(sk.rxcrypt.NonceSize())[m
[32m+[m	[32mdefer pool.GlobalPool.Put(nonce)[m
 	binary.BigEndian.PutUint64(nonce, sk.rxctr)[m
 	sk.rxctr++[m
[31m-	data, err := sk.rxcrypt.Open(nil, nonce, ciph, nil)[m
[32m+[m	[32mdata, err := sk.rxcrypt.Open(ciph[:0], nonce, ciph, nil)[m
 	if err != nil {[m
 		sk.rxerr = err[m
 		return[m
[36m@@ -128,29 +131,31 @@[m [mfunc (sk *Socket) Read(p []byte) (n int, err error) {[m
 [m
 // Write writes out the given byte slice. No guarantees are made regarding the number of low-level segments sent over the wire.[m
 func (sk *Socket) Write(p []byte) (n int, err error) {[m
[31m-	if len(p) > 32768 {[m
[32m+[m	[32mif len(p) > 65535 {[m
 		// recurse[m
 		var n1 int[m
 		var n2 int[m
[31m-		n1, err = sk.Write(p[:32768])[m
[32m+[m		[32mn1, err = sk.Write(p[:65535])[m
 		if err != nil {[m
 			return[m
 		}[m
[31m-		n2, err = sk.Write(p[32768:])[m
[32m+[m		[32mn2, err = sk.Write(p[65535:])[m
 		if err != nil {[m
 			return[m
 		}[m
 		n = n1 + n2[m
 		return[m
 	}[m
[32m+[m	[32mbackbuf := pool.GlobalPool.Get(2 + sk.txcrypt.Overhead() + len(p))[m
[32m+[m	[32mdefer pool.GlobalPool.Put(backbuf)[m
 	// main work here[m
[31m-	nonce := make([]byte, sk.txcrypt.NonceSize())[m
[32m+[m	[32mnonce := pool.GlobalPool.Get(sk.txcrypt.NonceSize())[m
[32m+[m	[32mdefer pool.GlobalPool.Put(nonce)[m
 	binary.BigEndian.PutUint64(nonce, sk.txctr)[m
 	sk.txctr++[m
[31m-	ciph := sk.txcrypt.Seal(nil, nonce, p, nil)[m
[31m-	lenbts := make([]byte, 2)[m
[31m-	binary.BigEndian.PutUint16(lenbts, uint16(len(ciph)))[m
[31m-	_, err = sk.plain.Write(append(lenbts, ciph...))[m
[32m+[m	[32mciph := sk.txcrypt.Seal(backbuf[2:][:0], nonce, p, nil)[m
[32m+[m	[32mbinary.BigEndian.PutUint16(backbuf[:2], uint16(len(ciph)))[m
[32m+[m	[32m_, err = sk.plain.Write(backbuf)[m
 	n = len(p)[m
 	return[m
 }[m
