package easyp2p

import (
	"encoding/binary"
	"errors"
	"fmt"
)

// P2PVersionType is the type of the protocol of P2P
type P2PVersionType uint32

const (
	p2pHeaderLength int = 8

	// P2PVersion1_0 : 1.0
	P2PVersion1_0 P2PVersionType = 1000
	// P2PVersion1_1 : 1.1
	P2PVersion1_1 P2PVersionType = 1100
	// P2PVersion2_0 : 2.0
	P2PVersion2_0 P2PVersionType = 2000

	// P2PVersionLatest is the alias to the latest version
	P2PVersionLatest = P2PVersion2_0
)

func (p P2PVersionType) String() string {
	return P2PVersionString(p)
}

// P2PVersionString converts P2PVersionType into string
func P2PVersionString(version P2PVersionType) string {
	s := fmt.Sprintf("%d.%d", version/1000, version%1000/100)

	if v := version % 100 / 10; v != 0 {
		s = fmt.Sprintf("%s.%d", s, v)
	}

	if v := version % 10; v != 0 {
		s = fmt.Sprintf("%s.%d", s, v)
	}

	return s
}

type p2pHeader struct {
	Version P2PVersionType
	// Encrypted bool: This must be true. (related issue: #1)
}

const (
	encryptedFlag byte = 1 << iota
)

func (h *p2pHeader) GetBytes() [p2pHeaderLength]byte {
	var b [p2pHeaderLength]byte

	binary.BigEndian.PutUint32(b[:], uint32(h.Version))

	/*var flag byte = 0
	if h.Encrypted {
		flag = flag | encryptedFlag
	}
	b[4] = flag*/

	return b
}

func parseP2PHeader(b []byte) (*p2pHeader, error) {
	if len(b) != p2pHeaderLength {
		return nil, errors.New("Illegal length. must be 8")
	}

	var header p2pHeader
	header.Version = P2PVersionType(binary.BigEndian.Uint32(b))

	/*flag := b[4]
	if (flag & encryptedFlag) != 0 {
		header.Encrypted = true
	}*/

	return &header, nil
}
