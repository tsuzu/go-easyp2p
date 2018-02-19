package easyp2p

import (
	"net"
	"sync/atomic"
)

type PacketConnIgnoreOK struct {
	ch    chan string
	addrs map[string]struct{}
	cnt   int

	net.PacketConn
}

func (wpc *PacketConnIgnoreOK) ReadFrom(b []byte) (int, net.Addr, error) {
	n, addr, err := wpc.PacketConn.ReadFrom(b)

	if err != nil {
		return n, addr, err
	}

	if n == 2 && string(b[:n]) == "OK" {
		if _, ok := wpc.addrs[addr.String()]; !ok {
			wpc.addrs[addr.String()] = struct{}{}

			if wpc.cnt < 10 {
				wpc.ch <- addr.String()
				wpc.cnt++
			}
		}

		return 0, addr, nil
	}

	return n, addr, nil
}

/*func (wpc *PacketConnIgnoreOK) WriteTo(b []byte, addr net.Addr) (int, error) {
	n, err := wpc.PacketConn.WriteTo(b, addr)

	log.Println("sent", string(b), addr, n, err)

	return n, err
}*/

type PacketConnChangeableCloserStatus struct {
	net.PacketConn
	status int32
}

func (p *PacketConnChangeableCloserStatus) SetStatus(enabled bool) {
	var v int32
	if enabled {
		v = 1
	}
	atomic.StoreInt32(&p.status, v)
}

func (p *PacketConnChangeableCloserStatus) Close() error {
	if atomic.LoadInt32(&p.status) == 1 {
		return p.PacketConn.Close()
	}

	return nil
}
