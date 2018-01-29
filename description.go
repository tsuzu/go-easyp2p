package easyp2p

import (
	"crypto/rand"
	"math/big"
)

const IdentifierLength int = 64

const letterBytes = "abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXYZ0123456789"

func RandomSecureIdentifier() string {
	b := make([]byte, 64)
	size := big.NewInt(int64(len(letterBytes)))
	for i := range b {
		c, _ := rand.Int(rand.Reader, size)
		b[i] = letterBytes[c.Int64()]
	}
	return string(b)
}

type Description struct {
	LocalAddresses []string
	Identifier     string
}
