package erand

import (
	"crypto/rand"
	"math/big"
)

// Int returns a cryptographically secure random number between 0 and max.
func Int(max int) int {
	b, err := rand.Int(rand.Reader, big.NewInt(int64(max)))
	if err != nil {
		panic(err)
	}
	return int(b.Int64())
}
