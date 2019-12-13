package niaucchi4

import (
	"net"
	"time"

	"golang.org/x/crypto/chacha20"
)

type ObfsStream struct {
	readCrypt  *chacha20.Cipher
	writeCrypt *chacha20.Cipher
	wbuf       []byte
	wire       net.Conn
}

func NewObfsStream(wire net.Conn, key []byte, isServ bool) *ObfsStream {
	os := &ObfsStream{}
	downNonce := make([]byte, 12)
	upNonce := make([]byte, 12)
	downNonce[0] = 1
	var err error

	os.readCrypt, err = chacha20.NewUnauthenticatedCipher(key, downNonce)
	if err != nil {
		panic(err)
	}
	os.writeCrypt, err = chacha20.NewUnauthenticatedCipher(key, upNonce)
	if err != nil {
		panic(err)
	}
	if isServ {
		tmp := os.readCrypt
		os.readCrypt = os.writeCrypt
		os.writeCrypt = tmp
	}
	os.wire = wire
	return os
}

func (os *ObfsStream) Write(p []byte) (n int, err error) {
	if len(os.wbuf) < len(p) {
		os.wbuf = make([]byte, len(p))
	}
	os.writeCrypt.XORKeyStream(os.wbuf[:len(p)], p)
	n, err = os.wire.Write(os.wbuf[:len(p)])
	return
}

func (os *ObfsStream) Read(p []byte) (n int, err error) {
	n, err = os.wire.Read(p)
	if err != nil {
		return
	}
	os.readCrypt.XORKeyStream(p[:n], p[:n])
	return
}

func (os *ObfsStream) Close() error {
	return os.wire.Close()
}

func (os *ObfsStream) LocalAddr() net.Addr {
	return os.wire.LocalAddr()
}

func (os *ObfsStream) RemoteAddr() net.Addr {
	return os.wire.RemoteAddr()
}

func (os *ObfsStream) SetDeadline(t time.Time) error {
	return os.wire.SetDeadline(t)
}

func (os *ObfsStream) SetReadDeadline(t time.Time) error {
	return os.wire.SetReadDeadline(t)
}

func (os *ObfsStream) SetWriteDeadline(t time.Time) error {
	return os.wire.SetWriteDeadline(t)
}
