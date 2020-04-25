package cshirt2

import (
	"crypto/rand"
	"crypto/subtle"
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"log"
	"sync"

	"net"
	"time"

	"github.com/geph-official/geph2/libs/erand"
	"github.com/minio/blake2b-simd"
	"github.com/patrickmn/go-cache"
	"golang.org/x/crypto/chacha20poly1305"
	"golang.org/x/crypto/curve25519"
)

// ErrAttackDetected denotes an error that can only happen when active probing attempts are made.
var ErrAttackDetected = errors.New("active probing attack detected")

// ErrBadHandshakeMAC denotes a bad handshake mac.
var ErrBadHandshakeMAC = errors.New("bad MAC in handshake")

func mac256(m, k []byte) []byte {
	mac := blake2b.NewMAC(32, k)
	mac.Write(m)
	return mac.Sum(nil)
}

func mac128(m, k []byte) []byte {
	mac := blake2b.NewMAC(16, k)
	mac.Write(m)
	return mac.Sum(nil)
}

var (
	globCache     = cache.New(time.Hour*3, time.Minute*30)
	globCacheLock sync.Mutex
)

const (
	pkSize = 192
)

func readPK(compatibility bool, secret []byte, isDown bool, transport net.Conn) (pubKey, int64, error) {
	epoch := time.Now().Unix() / 30
	// Read their public key
	theirPublic := make([]byte, pkSize)
	_, err := io.ReadFull(transport, theirPublic)
	if err != nil {
		return nil, 0, err
	}
	// new PK format: chacha20poly1305-encrypted ed25519 public key, with following two bytes denoting padding
	// try to decode as new PK format
	var edpk []byte
	var padlen uint16
	for e := epoch - 3; e < epoch+3; e++ {
		hsKey := mac256(secret, []byte(fmt.Sprintf("handshake-%v-%v", e, isDown)))
		crypt, _ := chacha20poly1305.New(hsKey)
		plain, er := crypt.Open(nil, make([]byte, 12), theirPublic, nil)
		if er != nil {
			err = er
			continue
		}
		edpk = plain[:32]
		padlen = binary.LittleEndian.Uint16(plain[32:][:2])
		epoch = e
	}
	if edpk != nil {
		if !replayFilter(edpk) {
			return nil, 0, ErrAttackDetected
		}
		// read past padding
		_, err = io.ReadFull(transport, make([]byte, padlen))
		if err != nil {
			err = fmt.Errorf("couldn't read past padding: %w", err)
			return nil, 0, err
		}
		return edpk, epoch, nil
	}
	if !compatibility {
		return nil, 0, errors.New("unrecognizable handshake")
	}
	// Read their public key MAC
	theirPublicMAC := make([]byte, 32)
	_, err = io.ReadFull(transport, theirPublicMAC)
	if err != nil {
		return nil, 0, err
	}
	macOK := false
	//shift := 0
	// shift one byte at a time until we find the mac
	for i := 0; i < 1024+(erand.Int(1024)); i++ {
		for e := epoch - 3; e < epoch+3; e++ {
			macKey := mac256(secret, []byte(fmt.Sprintf("%v", e)))
			if i > 0 && subtle.ConstantTimeCompare(theirPublicMAC, mac256(theirPublic, macKey)) == 1 {
				log.Printf("*** ΔE = %v sec, shift = %v ***", (e-epoch)*30, i)
				macOK = true
				epoch = e
				//shift = i
				goto out
			}
		}
		// read another byte
		oneBytes := make([]byte, 1)
		_, err = io.ReadFull(transport, oneBytes)
		if err != nil {
			return nil, 0, err
		}
		theirPublicMAC = append(theirPublicMAC, oneBytes...)[1:]
	}
	log.Println("** zero shift **", transport.RemoteAddr())
	return nil, 0, errors.New("zero shift")
out:
	if !replayFilter(theirPublic) {
		log.Printf("** replay attack detected ** %x %v", theirPublic[:10], transport.RemoteAddr())
		return nil, 0, ErrAttackDetected
	}
	log.Printf("-- GOOD %x %v", theirPublic[:10], transport.RemoteAddr())
	if !macOK {
		log.Println("** bad pk mac **", transport.RemoteAddr())
		return nil, 0, ErrBadHandshakeMAC
	}
	return theirPublic, epoch, nil
}

func replayFilter(pk []byte) bool {
	globCacheLock.Lock()
	defer globCacheLock.Unlock()
	if _, ok := globCache.Get(string(pk)); ok {
		return false
	}
	// Reject if bad
	globCache.SetDefault(string(pk), true)
	return true
}

func writePKLegacy(epoch int64, shift int, secret []byte, myPublic pubKey, transport net.Conn) error {
	if epoch == 0 {
		epoch = time.Now().Unix() / 30
	}
	macKey := mac256(secret, []byte(fmt.Sprintf("%v", epoch)))
	myPublicMAC := mac256(myPublic, macKey)
	padding := make([]byte, shift)
	rand.Read(padding)
	_, err := transport.Write(append(append(myPublic, padding...), myPublicMAC...))
	if err != nil {
		return err
	}
	return nil
}

func writePK(secret []byte, epoch int64, myPublic pubKey, isDown bool, transport net.Conn) error {
	if epoch == 0 {
		epoch = time.Now().Unix() / 30
	}
	hsPlain := make([]byte, 192-16)
	copy(hsPlain, myPublic)
	paddingAmount := erand.Int(65536)
	binary.LittleEndian.PutUint16(hsPlain[32:][:2], uint16(paddingAmount))
	hsKey := mac256(secret, []byte(fmt.Sprintf("handshake-%v-%v", epoch, isDown)))
	hsCrypter, _ := chacha20poly1305.New(hsKey)
	hsCrypt := hsCrypter.Seal(nil, make([]byte, 12), hsPlain, nil)
	padding := make([]byte, paddingAmount)
	rand.Read(padding)
	_, err := transport.Write(append(hsCrypt, padding...))
	return err
}

// Server negotiates obfuscation on a network connection, acting as the server. The secret must be provided.
func Server(secret []byte, compatibility bool, transport net.Conn) (net.Conn, error) {
	theirPK, epoch, err := readPK(compatibility, secret, false, transport)
	if err != nil {
		return nil, err
	}
	if len(theirPK) == 32 {
		mySK := make([]byte, 32)
		rand.Read(mySK)
		log.Printf("mySK = %x", mySK)
		myPK, err := curve25519.X25519(mySK, curve25519.Basepoint)
		if err != nil {
			panic(err)
		}
		writePK(secret, epoch, myPK, true, transport)
		log.Printf("myPK = %x, theirPK = %x", myPK, theirPK)
		shSecret, err := curve25519.X25519(mySK, theirPK)
		if err != nil {
			return nil, err
		}
		return newTransport(transport, shSecret, true), nil
	}
	myPK, mySK := dhGenKey()
	// if shift > 0 {
	// 	shift = erand.Int(1024)
	// }
	err = writePKLegacy(epoch, erand.Int(1000)+1, secret, myPK, transport)
	if err != nil {
		return nil, err
	}
	// Compute shared secret
	shSecret := udhSecret(mySK, theirPK)
	return newLegacyTransport(transport, shSecret, true), nil
}

// Client negotiates low-level obfuscation as a client, using the new protocol.
func Client(secret []byte, transport net.Conn) (net.Conn, error) {
	mySK := make([]byte, 32)
	rand.Read(mySK)
	myPK, err := curve25519.X25519(mySK, curve25519.Basepoint)
	if err != nil {
		panic(err)
	}
	err = writePK(secret, 0, myPK, false, transport)
	if err != nil {
		return nil, err
	}
	theirPK, _, err := readPK(false, secret, true, transport)
	if err != nil {
		return nil, err
	}
	if len(theirPK) != 32 {
		err = errors.New("wrong length for theirPK")
		return nil, err
	}
	shSecret, err := curve25519.X25519(mySK, theirPK)
	if err != nil {
		return nil, err
	}
	return newTransport(transport, shSecret, false), nil
}

// ClientLegacy negotiates low-level obfuscation as a client, using the legacy protocol.. The server
// secret must be given so that the client can prove knowledge.
func ClientLegacy(secret []byte, transport net.Conn) (net.Conn, error) {
	myPK, mySK := dhGenKey()
	err := writePKLegacy(0, erand.Int(1000)+1, secret, myPK, transport)
	if err != nil {
		return nil, err
	}
	theirPK, _, err := readPK(true, secret, true, transport)
	if err != nil {
		return nil, err
	}
	// Compute shared secret
	shSecret := udhSecret(mySK, theirPK)
	return newLegacyTransport(transport, shSecret, false), nil
}
