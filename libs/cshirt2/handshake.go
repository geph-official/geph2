package cshirt2

import (
	"crypto/subtle"
	"errors"
	"fmt"
	"io"
	"net"
	"time"

	"github.com/minio/blake2b-simd"
	"github.com/patrickmn/go-cache"
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
	globCache = cache.New(time.Minute*10, time.Minute*10)
)

func readPK(secret []byte, transport net.Conn) (dhPK, error) {
	// Read their public key
	theirPublic := make([]byte, 1536/8)
	_, err := io.ReadFull(transport, theirPublic)
	if err != nil {
		return nil, err
	}
	// Reject if bad
	if _, ok := globCache.Get(string(theirPublic)); ok {
		return nil, ErrAttackDetected
	}
	// Read their public key MAC
	theirPublicMAC := make([]byte, 32)
	_, err = io.ReadFull(transport, theirPublicMAC)
	if err != nil {
		return nil, err
	}
	macOK := false
	epoch := time.Now().Unix() / 30
	for e := epoch - 10; e < epoch+10; e++ {
		macKey := mac256(secret, []byte(fmt.Sprintf("%v", epoch)))
		if subtle.ConstantTimeCompare(theirPublicMAC, mac256(theirPublic, macKey)) == 1 {
			macOK = true
			break
		}
	}
	if !macOK {
		return nil, ErrBadHandshakeMAC
	}
	return theirPublic, nil
}

func writePK(secret []byte, myPublic dhPK, transport net.Conn) error {
	epoch := time.Now().Unix() / 30
	macKey := mac256(secret, []byte(fmt.Sprintf("%v", epoch)))
	myPublicMAC := mac256(myPublic, macKey)
	_, err := transport.Write(myPublic)
	if err != nil {
		return err
	}
	_, err = transport.Write(myPublicMAC)
	if err != nil {
		return err
	}
	return nil
}

// Server negotiates obfuscation on a network connection, acting as the server. The secret must be provided.
func Server(secret []byte, transport net.Conn) (net.Conn, error) {
	theirPK, err := readPK(secret, transport)
	if err != nil {
		return nil, err
	}
	myPK, mySK := dhGenKey()
	err = writePK(secret, myPK, transport)
	if err != nil {
		return nil, err
	}
	// Compute shared secret
	shSecret := udhSecret(mySK, theirPK)
	return newTransport(transport, shSecret, true), nil
}

// Client negotiates low-level obfuscation as a client. The server
// secret must be given so that the client can prove knowledge.
func Client(secret []byte, transport net.Conn) (net.Conn, error) {
	myPK, mySK := dhGenKey()
	err := writePK(secret, myPK, transport)
	if err != nil {
		return nil, err
	}
	theirPK, err := readPK(secret, transport)
	if err != nil {
		return nil, err
	}
	// Compute shared secret
	shSecret := udhSecret(mySK, theirPK)
	return newTransport(transport, shSecret, false), nil
}
