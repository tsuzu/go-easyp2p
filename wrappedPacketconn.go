package easyp2p

import (
	"net"
	"sync/atomic"
	"time"
)

type packetConnIgnoreOK struct {
	ch    chan string
	addrs map[string]struct{}
	cnt   int

	net.PacketConn
}

// len(b) must be >=2
func (wpc *packetConnIgnoreOK) ReadFrom(b []byte) (int, net.Addr, error) {
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

type validnessStatus int32

const (
	validnessEnabled validnessStatus = iota
	validnessCloserDisabled
	validnessDisabled
)

type packetConnChangeableValidness struct {
	PacketConn net.PacketConn
	status     int32
}

func (p *packetConnChangeableValidness) SetStatus(status validnessStatus) {
	atomic.StoreInt32(&p.status, int32(status))
}

func (p *packetConnChangeableValidness) ReadFrom(b []byte) (int, net.Addr, error) {
	switch validnessStatus(atomic.LoadInt32(&p.status)) {
	case validnessDisabled:
		return 0, nil, ErrDisconnected
	default:
	}

	return p.PacketConn.ReadFrom(b)
}

func (p *packetConnChangeableValidness) WriteTo(b []byte, addr net.Addr) (int, error) {
	switch validnessStatus(atomic.LoadInt32(&p.status)) {
	case validnessDisabled:
		return 0, ErrDisconnected
	default:
	}

	return p.PacketConn.WriteTo(b, addr)
}

func (p *packetConnChangeableValidness) Close() error {
	switch validnessStatus(atomic.LoadInt32(&p.status)) {
	case validnessEnabled:
		return p.PacketConn.Close()
	default:
	}

	return nil
}

func (p *packetConnChangeableValidness) LocalAddr() net.Addr {
	switch validnessStatus(atomic.LoadInt32(&p.status)) {
	case validnessDisabled:
		return nil
	default:
	}

	return p.PacketConn.LocalAddr()
}

func (p *packetConnChangeableValidness) SetDeadline(t time.Time) error {
	switch validnessStatus(atomic.LoadInt32(&p.status)) {
	case validnessDisabled:
		return nil
	default:
	}

	return p.PacketConn.SetDeadline(t)
}

func (p *packetConnChangeableValidness) SetReadDeadline(t time.Time) error {
	switch validnessStatus(atomic.LoadInt32(&p.status)) {
	case validnessDisabled:
		return nil
	default:
	}

	return p.PacketConn.SetReadDeadline(t)
}

func (p *packetConnChangeableValidness) SetWriteDeadline(t time.Time) error {
	switch validnessStatus(atomic.LoadInt32(&p.status)) {
	case validnessDisabled:
		return nil
	default:
	}

	return p.PacketConn.SetWriteDeadline(t)
}
