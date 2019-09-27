package niaucchi4

import (
	"bytes"
	"crypto/cipher"
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"encoding/binary"
	"errors"
	"fmt"
	mrand "math/rand"
	"time"

	"github.com/geph-official/geph2/libs/c25519"
	"golang.org/x/crypto/chacha20poly1305"
	"golang.org/x/crypto/curve25519"
)

func hm(m, k []byte) []byte {
	h := hmac.New(sha256.New, k)
	h.Write(m)
	return h.Sum(nil)
}

type tunstate struct {
	enc    cipher.AEAD
	dec    cipher.AEAD
	isserv bool
}

func (ts *tunstate) deriveKeys(ss []byte) {
	//log.Printf("deriving keys from shared state %x", ss[:5])
	upcrypt := aead(hm(ss, []byte("up")))
	dncrypt := aead(hm(ss, []byte("dn")))
	if ts.isserv {
		ts.enc = dncrypt
		ts.dec = upcrypt
	} else {
		ts.enc = upcrypt
		ts.dec = dncrypt
	}
}

func (ts *tunstate) Decrypt(pkt []byte) (bts []byte, err error) {
	if len(pkt) < chacha20poly1305.NonceSizeX {
		err = errors.New("no nonce found")
		return
	}
	bts, err = ts.dec.Open(nil, pkt[:chacha20poly1305.NonceSizeX], pkt[chacha20poly1305.NonceSizeX:], nil)
	return
}

func (ts *tunstate) Encrypt(pkt []byte) (ctext []byte) {
	nonce := make([]byte, chacha20poly1305.NonceSizeX)
	rand.Read(nonce)
	ctext = ts.enc.Seal(nonce, nonce, pkt, nil)
	return
}

type prototun struct {
	mySK   [32]byte
	cookie []byte
}

func (pt *prototun) realize(response []byte, isserv bool) (ts *tunstate, err error) {
	// decode their hello
	var theirHello helloPkt
	err = binary.Read(bytes.NewReader(response), binary.BigEndian, &theirHello)
	if err != nil {
		return
	}
	// create possible nowcookies
	for i := -3; i < 3; i++ {
		// derive nowcookie
		nowcookie := hm(pt.cookie, []byte(fmt.Sprintf("%v", time.Now().Unix()/30+int64(i))))
		//log.Printf("trying nowcookie %x", nowcookie[:5])
		theirPK, e := aead(hm(nowcookie, theirHello.Nonce[:])).
			Open(nil, make([]byte, chacha20poly1305.NonceSizeX), theirHello.EncPK[:], nil)
		if e != nil {
			continue
		}
		var sharedsec [32]byte
		var theirPKf [32]byte
		copy(theirPKf[:], theirPK)
		curve25519.ScalarMult(&sharedsec, &pt.mySK, &theirPKf)
		// make ts
		ts = &tunstate{
			isserv: isserv,
		}
		ts.deriveKeys(sharedsec[:])
		return
	}
	err = errors.New("none of the cookies work")
	return
}

func newproto(cookie []byte) (pt *prototun, hello []byte) {
	// derive nowcookie
	nowcookie := hm(cookie, []byte(fmt.Sprintf("%v", time.Now().Unix()/30)))
	//log.Printf("newproto with cookie = %x and nowcookie = %x", cookie[:5], nowcookie[:5])
	// generate keys
	sk := c25519.GenSK()
	var pk [32]byte
	curve25519.ScalarBaseMult(&pk, &sk)
	// create hello
	nonce := make([]byte, 32)
	rand.Read(nonce)
	encpk := aead(hm(nowcookie, nonce)).
		Seal(nil, make([]byte, chacha20poly1305.NonceSizeX), pk[:], nil)
	if len(encpk) != 48 {
		panic("encpk not 48 bytes long")
	}
	// form the pkt
	var tosend helloPkt
	copy(tosend.Nonce[:], nonce)
	copy(tosend.EncPK[:], encpk)
	buf := new(bytes.Buffer)
	binary.Write(buf, binary.BigEndian, tosend)
	padd := make([]byte, mrand.Int()%1000)
	buf.Write(padd)
	// return
	pt = &prototun{
		mySK:   sk,
		cookie: cookie,
	}
	hello = buf.Bytes()
	return
}

func aead(key []byte) cipher.AEAD {
	a, e := chacha20poly1305.NewX(key)
	if e != nil {
		panic(e)
	}
	return a
}

type helloPkt struct {
	Nonce [32]byte
	EncPK [48]byte
}
