package cshirt2

import (
	"bufio"
	"bytes"
	"crypto/cipher"
	"encoding/binary"
	"fmt"
	"io"
	"net"
	"time"

	"github.com/geph-official/geph2/libs/erand"
	pool "github.com/libp2p/go-buffer-pool"
	"golang.org/x/crypto/chacha20poly1305"
)

type transport struct {
	readCrypt  cipher.AEAD
	readNonce  uint64
	writeCrypt cipher.AEAD
	writeNonce uint64
	wireBuf    *bufio.Reader
	wire       net.Conn
	readbuf    bytes.Buffer

	buf [128]byte
}

const maxWriteSize = 16384

func (tp *transport) getiWriteNonce() uint64 {
	n := tp.writeNonce
	tp.writeNonce++
	return n
}

func (tp *transport) getiReadNonce() uint64 {
	n := tp.readNonce
	tp.readNonce++
	return n
}

func addPadding(appendTo, data []byte) []byte {
	padAmount := erand.Int(200)
	appendTo = append(appendTo, byte(padAmount))
	appendTo = append(appendTo, make([]byte, padAmount)...)
	appendTo = append(appendTo, data...)
	return appendTo
	//return data
}

func remPadding(data []byte) []byte {
	if len(data) > 0 && len(data) > int(data[0]) {
		return data[data[0]+1:]
	}
	return data
}

func (tp *transport) writeSegment(unpadded []byte) (err error) {
	toWrite := pool.Get(len(unpadded) + 256)[:0]
	defer pool.Put(toWrite)
	toWrite = addPadding(toWrite, unpadded)
	lengthNonce := tp.buf[:12]
	binary.LittleEndian.PutUint64(lengthNonce, tp.getiWriteNonce())
	bodyNonce := tp.buf[12:24]
	binary.LittleEndian.PutUint64(bodyNonce, tp.getiWriteNonce())
	length := tp.buf[24:26]
	binary.LittleEndian.PutUint16(length, uint16(len(toWrite)+tp.readCrypt.Overhead()))
	buffer := pool.Get(maxWriteSize + 128)[:0]
	defer pool.Put(buffer)
	buffer = tp.writeCrypt.Seal(buffer, lengthNonce, length, nil)
	buffer = tp.writeCrypt.Seal(buffer, bodyNonce, toWrite, nil)
	_, err = tp.wire.Write(buffer)
	return
}

func (tp *transport) Write(p []byte) (n int, err error) {
	ptr := p
	for len(ptr) > maxWriteSize {
		err = tp.writeSegment(ptr[:maxWriteSize])
		if err != nil {
			return
		}
		ptr = ptr[maxWriteSize:]
	}
	err = tp.writeSegment(ptr)
	if err != nil {
		return
	}
	n = len(p)
	return
}

func (tp *transport) Read(p []byte) (n int, err error) {
	for tp.readbuf.Len() == 0 {
		cryptLength := tp.buf[64:][:2+tp.readCrypt.Overhead()]
		_, err = io.ReadFull(tp.wireBuf, cryptLength)
		if err != nil {
			err = fmt.Errorf("can't read encrypted length: %w", err)
			return
		}
		nonce := tp.buf[32:][:12]
		binary.LittleEndian.PutUint64(nonce, tp.getiReadNonce())
		// decrypt length
		var length []byte
		length, err = tp.readCrypt.Open(p[:0], nonce, cryptLength, nil)
		if err != nil {
			err = fmt.Errorf("can't decrypt length: %w", err)
			return
		}
		// read body
		ctext := pool.Get(int(binary.LittleEndian.Uint16(length)))
		defer pool.Put(ctext)
		_, err = io.ReadFull(tp.wireBuf, ctext)
		if err != nil {
			err = fmt.Errorf("can't read body: %w", err)
			return
		}
		binary.LittleEndian.PutUint64(nonce, tp.getiReadNonce())
		var ptext []byte
		ptext, err = tp.readCrypt.Open(ctext[:0], nonce, ctext, nil)
		if err != nil {
			err = fmt.Errorf("can't decrypt body: %w", err)
			return
		}
		tp.readbuf.Write(remPadding(ptext))
	}
	return tp.readbuf.Read(p)
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

func (tp *transport) SetWriteDeadline(t time.Time) error {
	return tp.wire.SetWriteDeadline(t)
}

func (tp *transport) SetReadDeadline(t time.Time) error {
	return tp.wire.SetReadDeadline(t)
}

func newTransport(wire net.Conn, ss []byte, isServer bool) *transport {
	tp := new(transport)
	readKey := mac256(ss, []byte("c2s"))
	writeKey := mac256(ss, []byte("c2c"))
	if !isServer {
		readKey, writeKey = writeKey, readKey
	}
	var err error
	tp.readCrypt, err = chacha20poly1305.New(readKey)
	if err != nil {
		panic(err)
	}
	tp.writeCrypt, err = chacha20poly1305.New(writeKey)
	if err != nil {
		panic(err)
	}
	tp.wire = wire
	tp.wireBuf = bufio.NewReader(wire)
	return tp
}
