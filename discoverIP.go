package easyp2p

import (
	"fmt"
	"net"
	"strconv"
	"strings"
	"time"

	"github.com/anacrolix/utp"
	"github.com/gortc/stun"
)

type DiscoverIPFunc func(string, net.PacketConn) (string, error)

type connectionNopCloser struct {
	addr net.Addr
	conn net.PacketConn
}

func (c *connectionNopCloser) Read(b []byte) (int, error) {
	for {
		n, addr, err := c.conn.ReadFrom(b)

		if err != nil {
			return 0, err
		}

		if addr.String() == c.addr.String() {
			return n, nil
		}
	}
}

func (c *connectionNopCloser) Write(b []byte) (int, error) {
	return c.conn.WriteTo(b, c.addr)
}

func (c *connectionNopCloser) Close() error {
	return nil
}

func newConnectionNopCloser(conn net.PacketConn, addr string) (stun.Connection, error) {
	udp, err := net.ResolveUDPAddr("udp", addr)

	if err != nil {
		return nil, err
	}
	return &connectionNopCloser{
		conn: conn,
		addr: udp,
	}, nil
}

func DiscoverIPWithSTUN(addr string, conn net.PacketConn) (string, error) {
	c, err := newConnectionNopCloser(conn, addr)

	if err != nil {
		return "", err
	}
	client, err := stun.NewClient(stun.ClientOptions{
		Connection:  c,
		TimeoutRate: 100 * time.Millisecond,
	})
	conn.SetDeadline(time.Now().Add(time.Second))
	defer conn.SetDeadline(time.Time{})

	if err != nil {
		return "", err
	}

	defer client.Close()

	message := stun.MustBuild(stun.TransactionID, stun.BindingRequest)
	var retErr error
	var res string
	if err := client.Do(message, time.Now().Add(5*time.Second), func(event stun.Event) {
		if event.Error != nil {
			retErr = event.Error

			return
		}

		var xorAddr stun.XORMappedAddress
		if err := xorAddr.GetFrom(event.Message); err != nil {
			retErr = err

			return
		}

		res = xorAddr.IP.String() + ":" + strconv.FormatInt(int64(xorAddr.Port), 10)
	}); err != nil {
		return "", err
	}

	if retErr != nil {
		return "", retErr
	}

	return res, nil
}

func DiscoverIPSimple(addr string, conn net.PacketConn) (string, error) {
	s, err := utp.NewSocketFromPacketConnNoClose(conn)

	if err != nil {
		return "", err

	}
	defer s.CloseNow()

	c, err := s.Dial(addr)

	if err != nil {
		return "", err
	}

	defer c.Close()

	c.SetDeadline(time.Now().Add(4 * time.Second))

	c.Write([]byte(fmt.Sprintf("%s %s", IPDiscoveryRequestHeader, IPDiscoveryVersion)))

	b := make([]byte, 1024)
	n, err := c.Read(b)

	if err != nil {
		return "", err
	}

	b = b[:n]

	arr := strings.Split(string(b), " ")

	if len(arr) != 2 {
		return "", ErrUnknownProtocol
	}

	switch arr[0] {
	case IPDiscoveryResponseHeaderOK:
		return arr[1], nil
	case IPDiscoveryResponseHeaderProcotolError:
		return "", ErrUnknownProtocol
	default:
		return "", ErrUnknownProtocol
	}
}
