package c25519

import (
	"crypto/rand"
	"crypto/sha256"
)

// GenSK makes a new Curve25519 secret key.
func GenSK() [32]byte {
	var toret [32]byte
	rand.Read(toret[:])
	toret[0] &= 248
	toret[31] &= 127
	toret[31] |= 64
	return toret
}

// GenSKWithSeed makes a new Curve25519 secret key from a seed.
func GenSKWithSeed(seed []byte) [32]byte {
	toret := sha256.Sum256(seed)
	toret[0] &= 248
	toret[31] &= 127
	toret[31] |= 64
	return toret
}
