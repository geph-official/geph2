package cshirt2

import "encoding/binary"

// NewRNG creates a new RNG based on a seed.
func NewRNG(seed []byte) func() uint64 {
	seed = mac128(seed, seed)
	return func() uint64 {
		seed = mac128(seed, seed)
		return binary.LittleEndian.Uint64(seed[:8])
	}
}
