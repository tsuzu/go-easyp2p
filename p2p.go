package easyp2p

import (
	"context"
	"errors"
	"net"
	"strings"
	"sync"
	"time"

	"github.com/anacrolix/utp"
)

type P2PConn struct {
	udp     *net.UDPConn
	UTPConn net.Conn

	IPDiscoveryServers []string
	LocalAddresses     []string

	discoverIPFunc DiscoverIPFunc
}

func NewP2PConn(ipDiscoveryServers []string, discoverIP DiscoverIPFunc) *P2PConn {
	return &P2PConn{
		IPDiscoveryServers: ipDiscoveryServers,
		discoverIPFunc:     discoverIP,
	}
}

// If 0, ports will be automatically selected
func (conn *P2PConn) Listen(port int) (string, error) {
	udp, err := net.ListenUDP("udp", &net.UDPAddr{IP: net.IPv4zero, Port: port})

	if err != nil {
		return "", err
	}

	conn.udp = udp

	return udp.LocalAddr().String(), nil
}

// The first boolean value is whether one address at least is found  or not.
func (conn *P2PConn) DiscoverIP() (bool, error) {
	if conn.udp == nil {
		if _, err := conn.Listen(0); err != nil {
			return false, err
		}
	}

	var retErr []string
	leastOne := false

	addrs := make(map[string]struct{})

	if conn.discoverIPFunc != nil {
		for i := range conn.IPDiscoveryServers {
			server := conn.IPDiscoveryServers[i]

			addr, err := conn.discoverIPFunc(server, conn.udp)

			if err != nil {
				retErr = append(retErr, err.Error())

				continue
			}

			addrs[addr] = struct{}{}
			leastOne = true
		}
	}

	ifaces, err := net.Interfaces()

	if err != nil {
		retErr = append(retErr, err.Error())
	} else {
		port := strings.TrimPrefix(conn.udp.LocalAddr().String(), "[::]")

		for i := range ifaces {
			a, err := ifaces[i].Addrs()

			if err != nil {
				retErr = append(retErr, err.Error())

				continue
			}

			for i := range a {
				addr, ok := a[i].(*net.IPNet)

				if ok {
					switch {
					case addr.IP.To4() != nil:
						addrs[addr.IP.String()+port] = struct{}{}
						leastOne = true
					case addr.IP.To16() != nil:
					}
				}
			}
		}
	}

	strs := make([]string, 0, len(addrs))
	for k := range addrs {
		strs = append(strs, k)
	}

	conn.LocalAddresses = strs

	if len(retErr) != 0 {
		e := errors.New(strings.Join(retErr, ","))
		return leastOne, e
	}

	return leastOne, nil
}

func (conn *P2PConn) Connect(destAddrs []string, asServer bool) error {
	sock, err := utp.NewSocketFromPacketConnNoClose(&PacketConnIgnoreOK{conn.udp})

	if err != nil {
		return err
	}

	if asServer {
		go func() {
			for i := 0; i < 3; i++ {
				for i := range destAddrs {
					addr, err := net.ResolveUDPAddr("udp", destAddrs[i])
					if err == nil {
						sock.WriteTo([]byte("OK"), addr)
					}
				}
			}
		}()

		finCh := make(chan struct{})
		go func() {
			for {
				accepted, err := sock.Accept()

				select {
				case <-finCh:
					if accepted != nil {
						accepted.Close()
					}
					return
				default:
				}

				if err != nil {
					continue
				}

				go func() {
					found := false
					for i := range destAddrs {
						if destAddrs[i] == accepted.RemoteAddr().String() {
							found = true
							break
						}
					}

					if !found {
						accepted.Close()

						return
					}

					accepted.SetReadDeadline(time.Now().Add(5 * time.Second))

					b := make([]byte, 2)
					n, err := accepted.Read(b)

					if err != nil {
						accepted.Close()

						return
					}

					b = b[:n]

					if string(b) == "OK" {
						conn.UTPConn = accepted

						close(finCh)
					}

				}()
			}
		}()

		<-finCh

		sock.Close()

		return nil
	} else {
		finCh := make(chan struct{})
		triggerCh := make(chan struct{})

		var connectedConn net.Conn
		var mut sync.Mutex

		for i := range destAddrs {
			go func(daddr string) {
				pctx, cancel := context.WithCancel(context.Background())
				go func() {
					<-finCh
					cancel()
				}()
				defer cancel()
			FINISH_TRYING:
				for {
					ok := func() bool {
						ctx, cancel := context.WithTimeout(pctx, 5*time.Second)
						defer cancel()

						conn, err := sock.DialContext(ctx, daddr)

						if err != nil {
							return false
						}

						mut.Lock()
						if connectedConn == nil {
							connectedConn = conn

							triggerCh <- struct{}{}
						} else {
							conn.Close()
						}
						mut.Unlock()
						return true
					}()

					if ok {
						break
					}
					select {
					case <-finCh:
						break FINISH_TRYING
					default:
					}
				}
			}(destAddrs[i])
		}

		<-triggerCh
		close(finCh)

		if _, err := connectedConn.Write([]byte("OK")); err != nil {
			return err
		}

		conn.UTPConn = connectedConn

		return nil
	}
}

func (conn *P2PConn) Read(b []byte) (int, error) {
	if conn.UTPConn == nil {
		return 0, ErrNotConnected
	}

	return conn.UTPConn.Read(b)
}

func (conn *P2PConn) Write(b []byte) (int, error) {
	if conn.UTPConn == nil {
		return 0, ErrNotConnected
	}

	return conn.UTPConn.Write(b)
}

func (conn *P2PConn) Close() error {
	if conn.UTPConn != nil {
		conn.UTPConn.Close()
	}
	if conn.udp != nil {
		return conn.udp.Close()
	}

	return nil
}
