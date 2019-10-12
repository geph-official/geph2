package tinyss

import (
	"bytes"
	"crypto/cipher"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/binary"
	"io"
	"log"
	"net"
	"time"

	"github.com/geph-official/geph2/libs/c25519"
	"golang.org/x/crypto/chacha20poly1305"
	"golang.org/x/crypto/curve25519"
)

// Socket represents a TinySS connection; it implements net.Conn but with more methods.
type Socket struct {
	rxctr   uint64
	rxerr   error
	rxcrypt cipher.AEAD
	rxbuf   bytes.Buffer

	txctr   uint64
	txcrypt cipher.AEAD

	plain     net.Conn
	sharedsec []byte
}

func hm(m, k []byte) []byte {
	h := hmac.New(sha256.New, k)
	h.Write(m)
	return h.Sum(nil)
}

func aead(key []byte) cipher.AEAD {
	k, e := chacha20poly1305.New(key)
	if e != nil {
		panic(e)
	}
	return k
}

func newSocket(plain net.Conn, repk, lesk [32]byte) (sok *Socket, err error) {
	// calc
	var lepk [32]byte
	curve25519.ScalarBaseMult(&lepk, &lesk)
	// calculate shared secrets
	var sharedsec [32]byte
	curve25519.ScalarMult(&sharedsec, &lesk, &repk)
	s1 := hm(sharedsec[:], []byte("tinyss-s1"))
	s2 := hm(sharedsec[:], []byte("tinyss-s2"))
	// derive keys
	var rxkey []byte
	var txkey []byte
	if bytes.Compare(lepk[:], repk[:]) < 0 {
		rxkey = s1
		txkey = s2
	} else {
		txkey = s1
		rxkey = s2
	}
	// create socket
	sok = &Socket{
		rxcrypt:   aead(rxkey),
		txcrypt:   aead(txkey),
		plain:     plain,
		sharedsec: sharedsec[:],
	}
	// check that everything went well
	/*go func() {
		sok.Write(make([]byte, 16))
	}()
	_, err = sok.Read(make([]byte, 16))*/
	return
}

// Read reads into the given byte slice.
func (sk *Socket) Read(p []byte) (n int, err error) {
	// if any in buffer, read from buffer
	if sk.rxbuf.Len() > 0 {
		return sk.rxbuf.Read(p)
	}
	// if error exists, return it
	err = sk.rxerr
	if err != nil {
		return
	}
	// otherwise wait for record
	lenbts := make([]byte, 2)
	_, err = io.ReadFull(sk.plain, lenbts)
	if err != nil {
		sk.rxerr = err
		return
	}
	ciph := make([]byte, binary.BigEndian.Uint16(lenbts))
	_, err = io.ReadFull(sk.plain, ciph)
	if err != nil {
		sk.rxerr = err
		return
	}
	// decrypt the ciphertext
	nonce := make([]byte, sk.rxcrypt.NonceSize())
	binary.BigEndian.PutUint64(nonce, sk.rxctr)
	sk.rxctr++
	data, err := sk.rxcrypt.Open(nil, nonce, ciph, nil)
	if err != nil {
		sk.rxerr = err
		return
	}
	// copy the data into the buffer
	n = copy(p, data)
	if n < len(data) {
		sk.rxbuf.Write(data[n:])
	}
	return
}

// Write writes out the given byte slice. No guarantees are made regarding the number of low-level segments sent over the wire.
func (sk *Socket) Write(p []byte) (n int, err error) {
	if len(p) > 32768 {
		// recurse
		var n1 int
		var n2 int
		n1, err = sk.Write(p[:32768])
		if err != nil {
			return
		}
		n2, err = sk.Write(p[32768:])
		if err != nil {
			return
		}
		n = n1 + n2
		return
	}
	// main work here
	nonce := make([]byte, sk.txcrypt.NonceSize())
	binary.BigEndian.PutUint64(nonce, sk.txctr)
	sk.txctr++
	ciph := sk.txcrypt.Seal(nil, nonce, p, nil)
	lenbts := make([]byte, 2)
	binary.BigEndian.PutUint16(lenbts, uint16(len(ciph)))
	_, err = sk.plain.Write(append(lenbts, ciph...))
	n = len(p)
	return
}

// Close closes the socket.
func (sk *Socket) Close() error {
	return sk.plain.Close()
}

// LocalAddr returns the local address.
func (sk *Socket) LocalAddr() net.Addr {
	return sk.plain.LocalAddr()
}

// RemoteAddr returns the remote address.
func (sk *Socket) RemoteAddr() net.Addr {
	return sk.plain.RemoteAddr()
}

// SetDeadline sets the deadline.
func (sk *Socket) SetDeadline(t time.Time) error {
	return sk.plain.SetDeadline(t)
}

// SetReadDeadline sets the read deadline.
func (sk *Socket) SetReadDeadline(t time.Time) error {
	return sk.plain.SetReadDeadline(t)
}

// SetWriteDeadline sets the write deadline.
func (sk *Socket) SetWriteDeadline(t time.Time) error {
	return sk.plain.SetWriteDeadline(t)
}

// SharedSec returns the shared secret. Use this to authenticate the connection (through signing etc).
func (sk *Socket) SharedSec() []byte {
	return sk.sharedsec
}

// Handshake upgrades a plaintext socket to a MiniSS socket, given our secret key.
func Handshake(plain net.Conn) (sok *Socket, err error) {
	// generate ephemeral key
	myesk := c25519.GenSK()
	// in another thread, send over hello
	wet := make(chan bool)
	go func() {
		var msgb bytes.Buffer
		msgb.Write([]byte("TinySS-1"))
		var pub [32]byte
		curve25519.ScalarBaseMult(&pub, &myesk)
		msgb.Write(pub[:])
		io.Copy(plain, &msgb)
		log.Println("WROTE")
		close(wet)
	}()
	// read hello
	bts := make([]byte, 32+8)
	_, err = io.ReadFull(plain, bts)
	if err != nil {
		return
	}
	// check version
	if string(bts[:8]) != "TinySS-1" {
		err = io.ErrClosedPipe
		return
	}
	// read rest of hello
	bts = bts[8:]
	<-wet
	var repk [32]byte
	copy(repk[:], bts[:32])
	return newSocket(plain, repk, myesk)
}
