package easyp2p

import (
	"crypto/rand"
	"math/big"
)

const identifierLength int = 64

const letterBytes = "abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXYZ0123456789"

func randomSecureIdentifier() string {
	b := make([]byte, 64)
	size := big.NewInt(int64(len(letterBytes)))
	for i := range b {
		c, _ := rand.Int(rand.Reader, size)
		b[i] = letterBytes[c.Int64()]
	}
	return string(b)
}

// Description is the type of local and remote description given between two peers
type Description struct {
	ProtocolVersion P2PVersionType
	LocalAddresses  []string
	Identifier      string
	CAPEM           []byte
}
