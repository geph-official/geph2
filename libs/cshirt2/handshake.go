package cshirt2

import (
	"crypto/rand"
	"crypto/subtle"
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

func readPK(secret []byte, transport net.Conn) (dhPK, int64, int, error) {
	// Read their public key
	theirPublic := make([]byte, 1536/8)
	_, err := io.ReadFull(transport, theirPublic)
	if err != nil {
		confusinglySleep()
		return nil, 0, 0, err
	}
	// Read their public key MAC
	theirPublicMAC := make([]byte, 32)
	_, err = io.ReadFull(transport, theirPublicMAC)
	if err != nil {
		confusinglySleep()
		return nil, 0, 0, err
	}
	macOK := false
	epoch := time.Now().Unix() / 30
	shift := 0
	// shift one byte at a time until we find the mac
	for i := 0; i < 1024+(erand.Int(1024)); i++ {
		for e := epoch - 10; e < epoch+10; e++ {
			macKey := mac256(secret, []byte(fmt.Sprintf("%v", e)))
			if i > 0 && subtle.ConstantTimeCompare(theirPublicMAC, mac256(theirPublic, macKey)) == 1 {
				//log.Printf("*** ΔE = %v sec, shift = %v ***", (e-epoch)*30, i)
				macOK = true
				epoch = e
				shift = i
				goto out
			}
		}
		// read another byte
		oneBytes := make([]byte, 1)
		_, err = io.ReadFull(transport, oneBytes)
		if err != nil {
			confusinglySleep()
			return nil, 0, 0, err
		}
		theirPublicMAC = append(theirPublicMAC, oneBytes...)[1:]
	}
	log.Println("** zero shift **", transport.RemoteAddr())
	confusinglySleep()
	return nil, 0, 0, errors.New("zero shift")
out:
	globCacheLock.Lock()
	if _, ok := globCache.Get(string(theirPublic)); ok {
		globCacheLock.Unlock()
		log.Printf("** replay attack detected ** %x %v", theirPublic[:10], transport.RemoteAddr())
		confusinglySleep()
		return nil, 0, 0, ErrAttackDetected
	}
	log.Printf("-- GOOD %x %v", theirPublic[:10], transport.RemoteAddr())
	// Reject if bad
	globCache.SetDefault(string(theirPublic), true)
	globCacheLock.Unlock()
	if !macOK {
		log.Println("** bad pk mac **", transport.RemoteAddr())
		confusinglySleep()
		return nil, 0, 0, ErrBadHandshakeMAC
	}
	return theirPublic, epoch, shift, nil
}

func writePK(epoch int64, shift int, secret []byte, myPublic dhPK, transport net.Conn) error {
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

// Server negotiates obfuscation on a network connection, acting as the server. The secret must be provided.
func Server(secret []byte, transport net.Conn) (net.Conn, error) {
	theirPK, epoch, _, err := readPK(secret, transport)
	if err != nil {
		return nil, err
	}
	myPK, mySK := dhGenKey()
	// if shift > 0 {
	// 	shift = erand.Int(1024)
	// }
	err = writePK(epoch, erand.Int(1000)+1, secret, myPK, transport)
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
	err := writePK(0, erand.Int(1000)+1, secret, myPK, transport)
	if err != nil {
		return nil, err
	}
	theirPK, _, _, err := readPK(secret, transport)
	if err != nil {
		return nil, err
	}
	// Compute shared secret
	shSecret := udhSecret(mySK, theirPK)
	return newTransport(transport, shSecret, false), nil
}
