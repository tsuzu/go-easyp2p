package easyp2p

import (
	"net"
)

type PacketConnIgnoreOK struct {
	net.PacketConn
}

func (wpc *PacketConnIgnoreOK) ReadFrom(b []byte) (int, net.Addr, error) {
	n, addr, err := wpc.PacketConn.ReadFrom(b)

	if err != nil {
		return n, addr, err
	}

	if n == 2 && string(b[:n]) == "OK" {
		return 0, addr, nil
	}

	return n, addr, nil
}

/*func (wpc *PacketConnIgnoreOK) WriteTo(b []byte, addr net.Addr) (int, error) {
	n, err := wpc.PacketConn.WriteTo(b, addr)

	log.Println("sent", string(b), addr, n, err)

	return n, err
}*/
