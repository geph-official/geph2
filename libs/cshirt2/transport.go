package cshirt2

import (
	"bytes"
	"crypto/cipher"
	"crypto/subtle"
	"encoding/binary"
	"errors"
	"io"
	mrand "math/rand"
	"net"
	"time"

	"golang.org/x/crypto/chacha20"
)

// generates padding, given a write size
func generatePadding(wsize int) []byte {
	// TODO improve
	if wsize > 3000 {
		return nil
	}
	return make([]byte, mrand.Int()%512)
}

type transport struct {
	readMAC    []byte
	readCrypt  cipher.Stream
	writeMAC   []byte
	writeCrypt cipher.Stream
	wire       net.Conn
	readbuf    bytes.Buffer
}

func (tp *transport) Read(b []byte) (n int, err error) {
	for tp.readbuf.Len() == 0 {
		// read the mac
		macBts := make([]byte, 16)
		_, err = io.ReadFull(tp.wire, macBts)
		if err != nil {
			return
		}
		// read the *encrypted* payload length
		cryptPayloadLenBts := make([]byte, 2)
		_, err = io.ReadFull(tp.wire, cryptPayloadLenBts)
		if err != nil {
			return
		}
		plainPayloadLenBts := make([]byte, 2)
		tp.readCrypt.XORKeyStream(plainPayloadLenBts, cryptPayloadLenBts)
		// read the encrypted payload
		cryptInnerPayloadBts := make([]byte, int(binary.BigEndian.Uint16(plainPayloadLenBts)))
		_, err = io.ReadFull(tp.wire, cryptInnerPayloadBts)
		if err != nil {
			return
		}
		// verify the MAC
		toMAC := append(cryptPayloadLenBts, cryptInnerPayloadBts...)
		if subtle.ConstantTimeCompare(macBts, mac128(toMAC, tp.readMAC)) != 1 {
			err = errors.New("MAC error")
			return
		}
		tp.readMAC = mac256(tp.readMAC, nil)
		// decrypt the payload itself
		plainInnerPayloadBts := make([]byte, len(cryptInnerPayloadBts))
		tp.readCrypt.XORKeyStream(plainInnerPayloadBts, cryptInnerPayloadBts)
		if len(plainInnerPayloadBts) < 2 {
			err = errors.New("truncated payload")
			return
		}
		// get the non-padding part
		realLenBts := plainInnerPayloadBts[:2]
		realBts := plainInnerPayloadBts[2:][:binary.BigEndian.Uint16(realLenBts)]
		// stuff the payload into the read buffer
		tp.readbuf.Write(realBts)
	}
	n, err = tp.readbuf.Read(b)
	return
}

func (tp *transport) Write(b []byte) (n int, err error) {
	if len(b) > 65535 {
		panic("don't know what to do!")
	}
	// first generate the plaintext payload
	plainBuf := new(bytes.Buffer)
	padding := generatePadding(len(b))
	binary.Write(plainBuf, binary.BigEndian, uint16(len(padding)+len(b)+2))
	binary.Write(plainBuf, binary.BigEndian, uint16(len(b)))
	plainBuf.Write(b)
	plainBuf.Write(padding)
	// then we encrypt the payload
	cryptPayload := plainBuf.Bytes()
	tp.writeCrypt.XORKeyStream(cryptPayload, cryptPayload)
	// then we compute the MAC and ratchet forward the key
	mac := mac128(cryptPayload, tp.writeMAC)
	tp.writeMAC = mac256(tp.writeMAC, nil)
	// then we assemble everything
	_, err = tp.wire.Write(append(mac, cryptPayload...))
	if err != nil {
		return
	}
	n = len(b)
	return
}

func (tp *transport) Close() error {
	return tp.wire.Close()
}

func (tp *transport) LocalAddr() net.Addr {
	return tp.wire.LocalAddr()
}

func (tp *transport) RemoteAddr() net.Addr {
	return tp.wire.RemoteAddr()
}

func (tp *transport) SetDeadline(t time.Time) error {
	return tp.wire.SetDeadline(t)
}

func (tp *transport) SetReadDeadline(t time.Time) error {
	return tp.wire.SetReadDeadline(t)
}

func (tp *transport) SetWriteDeadline(t time.Time) error {
	return tp.wire.SetWriteDeadline(t)
}

func newTransport(wire net.Conn, ss []byte, isServer bool) *transport {
	tp := new(transport)
	readKey := mac256(ss, []byte("c2s"))
	writeKey := mac256(ss, []byte("c2c"))
	if !isServer {
		readKey, writeKey = writeKey, readKey
	}
	var err error
	tp.readMAC = mac256(readKey, []byte("mac"))
	tp.readCrypt, err = chacha20.NewUnauthenticatedCipher(mac256(readKey, []byte("crypt")), make([]byte, 12))
	if err != nil {
		panic(err)
	}
	tp.writeMAC = mac256(writeKey, []byte("mac"))
	tp.writeCrypt, err = chacha20.NewUnauthenticatedCipher(mac256(writeKey, []byte("crypt")), make([]byte, 12))
	if err != nil {
		panic(err)
	}
	tp.wire = wire
	return tp
}
