package cshirt2

import (
	"crypto/rand"
	"math/big"
)

var dhGroup5 = func() *big.Int {
	toret := big.NewInt(0)
	toret.SetString("FFFFFFFFFFFFFFFFC90FDAA22168C234C4C6628B80DC1CD129024E088A67CC74020BBEA63B139B22514A08798E3404DDEF9519B3CD3A431B302B0A6DF25F14374FE1356D6D51C245E485B576625E7EC6F44C42E9A637ED6B0BFF5CB6F406B7EDEE386BFB5A899FA5AE9F24117C4B1FE649286651ECE45B3DC2007CB8A163BF0598DA48361C55D39A69163FA8FD24CF5F83655D23DCA3AD961C62F356208552BB9ED529077096966D670C354E4ABC9804F1746C08CA237327FFFFFFFFFFFFFFFF", 16)
	return toret
}()

type dhPK []byte
type dhSK []byte

func udhSecret(lsk dhSK, rpk dhPK) []byte {
	bitlen := len(lsk) * 8
	// checks
	if bitlen != 1536 {
		panic("Why are you trying to generate DH key with wrong bitlen?")
	}
	var group *big.Int
	group = dhGroup5
	return big.NewInt(0).Exp(big.NewInt(0).SetBytes(rpk),
		big.NewInt(0).SetBytes(lsk), group).Bytes()
}

func dhGenKey() (pk dhPK, sk dhSK) {
	const bitlen = 1536
	var group *big.Int
	group = dhGroup5
	// randomly generate even private key
	pub := dhPK(make([]byte, bitlen/8))
	priv := dhSK(make([]byte, bitlen/8))
	rand.Read(priv)
	priv[bitlen/8-1] /= 2
	priv[bitlen/8-1] *= 2
	privBnum := big.NewInt(0).SetBytes(priv)
retry:
	// generate public key
	pubBnum := big.NewInt(0).Exp(big.NewInt(2), privBnum, group)
	ggg := make([]byte, 1)
	rand.Read(ggg)
	if ggg[0]%2 == 0 {
		pubBnum = big.NewInt(0).Sub(group, pubBnum)
	}
	// Obtain pubkey
	candid := pubBnum.Bytes()
	if len(candid) != len(pub) {
		goto retry
	}
	copy(pub, candid)
	return pub, priv
}
