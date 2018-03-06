package easyp2p

import (
	"encoding/binary"
	"errors"
	"fmt"
)

const (
	P2PHeaderLength int = 8

	P2PVersion1_0 uint32 = 1000
	P2PVersion1_1 uint32 = 1100
	P2PVersion2_0 uint32 = 2000

	P2PVersionLatest = P2PVersion2_0
)

func P2PVersionString(version uint32) string {
	s := fmt.Sprintf("%d.%d", version/1000, version%1000/100)

	if v := version % 100 / 10; v != 0 {
		s = fmt.Sprintf("%s.%d", s, v)
	}

	if v := version % 10; v != 0 {
		s = fmt.Sprintf("%s.%d", s, v)
	}

	return s
}

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
