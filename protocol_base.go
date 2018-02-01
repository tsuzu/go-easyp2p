package easyp2p

import (
	"encoding/binary"
	"errors"
)

const (
	P2PHeaderLength int = 8

	P2PVersion1_0 uint32 = 1000
	P2PVersion1_1 uint32 = 1100

	P2PVersionLatest = P2PVersion1_1
)

type P2PHeader struct {
	Version uint32
	// Encrypted bool: This must be true. (related issue: #1)
}

const (
	encryptedFlag byte = 1 << iota
)

func (h *P2PHeader) GetBytes() [P2PHeaderLength]byte {
	var b [P2PHeaderLength]byte

	binary.BigEndian.PutUint32(b[:], h.Version)

	/*var flag byte = 0
	if h.Encrypted {
		flag = flag | encryptedFlag
	}
	b[4] = flag*/

	return b
}

func ParseP2PHeader(b []byte) (*P2PHeader, error) {
	if len(b) != P2PHeaderLength {
		return nil, errors.New("Illegal length. must be 8")
	}

	var header P2PHeader
	header.Version = binary.BigEndian.Uint32(b)

	/*flag := b[4]
	if (flag & encryptedFlag) != 0 {
		header.Encrypted = true
	}*/

	return &header, nil
}
