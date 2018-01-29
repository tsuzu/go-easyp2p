package easyp2p

import (
	"encoding/binary"
	"errors"
)

const (
	P2PHeaderLength   int    = 8
	P2PCurrentVersion uint32 = 1
)

type P2PHeader struct {
	Version   uint32
	Encrypted bool
}

const (
	encryptedFlag byte = 1 << iota
)

func (h *P2PHeader) GetBytes() [P2PHeaderLength]byte {
	var b [P2PHeaderLength]byte

	binary.BigEndian.PutUint32(b[:], h.Version)

	var flag byte = 0
	if h.Encrypted {
		flag = flag | encryptedFlag
	}
	b[4] = flag

	return b
}

func ParseP2PHeader(b []byte) (*P2PHeader, error) {
	if len(b) != P2PHeaderLength {
		return nil, errors.New("Illegal length. must be 8")
	}

	var header P2PHeader
	header.Version = binary.BigEndian.Uint32(b)

	flag := b[4]
	if (flag & encryptedFlag) != 0 {
		header.Encrypted = true
	}

	return &header, nil
}
