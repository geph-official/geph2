package cryptrr

import (
	"crypto/rand"
	"crypto/sha256"
	"fmt"

	"github.com/ethereum/go-ethereum/rlp"
	"golang.org/x/crypto/chacha20poly1305"
	"golang.org/x/crypto/curve25519"
)

// PlainMsg is a plaintext message.
type PlainMsg struct {
	Cmd  string
	Args rlp.RawValue
}

// NewPlainMsg constructs a plain message.
func NewPlainMsg(cmd string, args ...interface{}) PlainMsg {
	aa, err := rlp.EncodeToBytes(args)
	if err != nil {
		panic(err)
	}
	return PlainMsg{
		Cmd:  cmd,
		Args: rlp.RawValue(aa),
	}
}

// CiphMsg is an enciphered message.
type CiphMsg struct {
	LocalPK [32]byte
	Nonce   [32]byte
	Ctext   []byte
}

// Encrypt encrypts a PlainMsg to be decoded by someone holding the secret key to remotePK.
func (pmsg PlainMsg) Encrypt(localSK [32]byte, remotePK [32]byte, isServer bool) CiphMsg {
	payload, err := rlp.EncodeToBytes(pmsg)
	if err != nil {
		panic(err)
	}
	ss, err := curve25519.X25519(localSK[:], remotePK[:])
	if err != nil {
		panic(err)
	}
	var nonce [32]byte
	rand.Read(nonce[:])
	key := sha256.Sum256(append(nonce[:], append([]byte(fmt.Sprint(isServer)), ss...)...))
	aead, err := chacha20poly1305.New(key[:])
	if err != nil {
		panic(err)
	}
	ctext := aead.Seal(nil, make([]byte, 12), payload, nil)
	var localPK [32]byte
	curve25519.ScalarBaseMult(&localPK, &localSK)
	return CiphMsg{
		LocalPK: localPK,
		Nonce:   nonce,
		Ctext:   ctext,
	}
}
