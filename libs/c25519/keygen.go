package c25519

import "crypto/rand"

// GenSK makes a new Curve25519 secret key.
func GenSK() [32]byte {
	var toret [32]byte
	rand.Read(toret[:])
	toret[0] &= 248
	toret[31] &= 127
	toret[31] |= 64
	return toret
}
