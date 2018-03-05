package easyp2p

import (
	"net"
	"sync"
)

type filterPacketConn struct {
	net.PacketConn

	mut     sync.RWMutex
	allowed string
}

func newFilterPacketConn(pc net.PacketConn) *filterPacketConn {
	return &filterPacketConn{
		PacketConn: pc,
	}
}

func (fpc *filterPacketConn) ReadFrom(b []byte) (int, net.Addr, error) {
	for {
		n, addr, err := fpc.PacketConn.ReadFrom(b)

		fpc.mut.RLock()
		if len(fpc.allowed) == 0 || addr.String() == fpc.allowed {
			fpc.mut.RUnlock()
			return n, addr, err
		}
		fpc.mut.RUnlock()

		if err != nil {
			return 0, nil, err
		}
	}
}

func (fpc *filterPacketConn) WriteTo(b []byte, addr net.Addr) (int, error) {
	return fpc.PacketConn.WriteTo(b, addr)
}

// If empty, all packets will be passed
func (fpc *filterPacketConn) setAllowedAddress(addr string) {
	fpc.mut.Lock()
	fpc.allowed = addr
	fpc.mut.Unlock()
}
