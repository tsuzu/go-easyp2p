package easyp2p

import (
	"context"
	"crypto/tls"
	"encoding/json"
	"errors"
	"io"
	"io/ioutil"
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
	TLSEncryption      bool
	CertificateFunc    GetCertificateFuncType

	identifier     string
	discoverIPFunc DiscoverIPFunc
}

func NewP2PConn(ipDiscoveryServers []string, discoverIP DiscoverIPFunc) *P2PConn {
	return &P2PConn{
		IPDiscoveryServers: ipDiscoveryServers,
		discoverIPFunc:     discoverIP,
		TLSEncryption:      true,
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

func (conn *P2PConn) Connect(remoteDescriptionString string, asServer bool, ctx context.Context) error {
	var remoteDescription Description
	if err := json.Unmarshal([]byte(remoteDescriptionString), &remoteDescription); err != nil {
		return err
	}

	wrapped := &PacketConnIgnoreOK{
		PacketConn: conn.udp,
	}

	if !asServer {
		wrapped.addrs = make(map[string]struct{})

		for _, msg := range remoteDescription.LocalAddresses {
			wrapped.addrs[msg] = struct{}{}
		}
		wrapped.ch = make(chan string, 1024)
	}

	sock, err := utp.NewSocketFromPacketConnNoClose(wrapped)

	if err != nil {
		return err
	}

	if asServer {
		go func() {
			for i := 0; i < 3; i++ {
				for i := range remoteDescription.LocalAddresses {
					addr, err := net.ResolveUDPAddr("udp", remoteDescription.LocalAddresses[i])
					if err == nil {
						sock.WriteTo([]byte("OK"), addr)
					}
				}
			}
		}()

		var config *tls.Config

		if conn.TLSEncryption {
			if conn.CertificateFunc == nil {
				conn.CertificateFunc = NewSelfSignedCertificate
			}

			cert, err := conn.CertificateFunc()

			if err != nil {
				sock.Close()

				return err
			}

			config = &tls.Config{
				Certificates: []tls.Certificate{cert},
			}
		}

		finCh := make(chan struct{})
		var wg sync.WaitGroup
		wg.Add(1)
		go func() {
			defer wg.Done()
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

				wg.Add(1)
				go func() {
					defer wg.Done()
					accepted.SetDeadline(time.Now().Add(5 * time.Second))

					// Send header
					hbytes := (&P2PHeader{
						Version:   P2PCurrentVersion,
						Encrypted: conn.TLSEncryption,
					}).GetBytes()
					if _, err := accepted.Write(hbytes[:]); err != nil {
						accepted.Close()

						return
					}

					// Read header
					if _, err := accepted.Read(hbytes[:]); err != nil {
						accepted.Close()

						return
					}

					var header *P2PHeader
					if header, err = ParseP2PHeader(hbytes[:]); err != nil {
						accepted.Close()

						return
					}
					if header.Version != P2PCurrentVersion {
						accepted.Close()

						return
					}
					if !conn.TLSEncryption && header.Encrypted {
						accepted.Close()

						return
					}

					var newConn net.Conn

					if header.Encrypted {
						newConn = tls.Server(accepted, config)
					} else {
						newConn = accepted
					}

					newConn.SetDeadline(time.Now().Add(5 * time.Second))

					b, err := ioutil.ReadAll(&io.LimitedReader{R: newConn, N: int64(IdentifierLength)})
					if err != nil {
						newConn.Close()

						return
					}
					if len(b) != IdentifierLength {
						newConn.Close()

						return
					}
					if string(b) != conn.identifier {
						newConn.Close()

						return
					}

					if _, err := newConn.Write([]byte(remoteDescription.Identifier)); err != nil {
						newConn.Close()

						return
					}

					b = make([]byte, 2)
					n, err := newConn.Read(b)

					if err != nil {
						newConn.Close()

						return
					}

					b = b[:n]
					newConn.SetDeadline(time.Time{})

					if string(b) == "OK" {
						conn.UTPConn = newConn

						close(finCh)
					} else {
						newConn.Close()
					}

				}()
			}
		}()
		select {
		case <-finCh:
			sock.Close()
		case <-ctx.Done():
			sock.CloseNow()
			wg.Wait()
			if conn.UTPConn != nil {
				conn.UTPConn.Close()
			}

			return ctx.Err()
		}

		return nil
	} else {
		finCh := make(chan struct{})
		triggerCh := make(chan struct{})

		var finalConencted net.Conn
		var mut sync.Mutex

		var wg sync.WaitGroup
		wg.Add(1)
		go func() {
			defer wg.Done()
			ch := make(chan string, len(remoteDescription.LocalAddresses))

			for i := range remoteDescription.LocalAddresses {
				ch <- remoteDescription.LocalAddresses[i]
			}

			for {
				var addr string
				select {
				case addr = <-ch:
					break
				case addr = <-wrapped.ch:
					break
				case <-finCh:
					return
				}
				wg.Add(1)
				go func(daddr string) {
					defer wg.Done()
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

							connected, err := sock.DialContext(ctx, daddr)

							if err != nil {
								return false
							}

							connected.SetDeadline(time.Now().Add(5 * time.Second))

							// Read header
							var hbytes [P2PHeaderLength]byte
							if _, err := connected.Read(hbytes[:]); err != nil {
								connected.Close()

								return false
							}

							var header *P2PHeader
							if header, err = ParseP2PHeader(hbytes[:]); err != nil {
								connected.Close()

								return false
							}
							if header.Version != P2PCurrentVersion {
								connected.Close()

								return false
							}

							encrypted := conn.TLSEncryption && header.Encrypted
							// Send header
							hbytes = (&P2PHeader{
								Version:   P2PCurrentVersion,
								Encrypted: encrypted,
							}).GetBytes()
							if _, err := connected.Write(hbytes[:]); err != nil {
								connected.Close()

								return false
							}

							var newConn net.Conn

							if encrypted {
								newConn = tls.Client(connected, &tls.Config{InsecureSkipVerify: true})
							} else {
								newConn = connected
							}

							if _, err := newConn.Write([]byte(remoteDescription.Identifier)); err != nil {
								newConn.Close()

								return false
							}

							newConn.SetDeadline(time.Now().Add(5 * time.Second))

							b, err := ioutil.ReadAll(&io.LimitedReader{R: newConn, N: int64(IdentifierLength)})
							if err != nil {
								newConn.Close()

								return false
							}

							if len(b) != IdentifierLength {
								newConn.Close()

								return false
							}
							if string(b) != conn.identifier {
								newConn.Close()

								return false
							}

							newConn.SetDeadline(time.Time{})

							mut.Lock()
							if finalConencted == nil {
								finalConencted = newConn

								triggerCh <- struct{}{}
							} else {
								newConn.Close()
							}
							mut.Unlock()
							return true
						}()

						if ok {
							break FINISH_TRYING
						}
						select {
						case <-finCh:
							break FINISH_TRYING
						default:
						}
					}
				}(addr)
			}
		}()

		select {
		case <-triggerCh:
			close(finCh)

		case <-ctx.Done():
			close(finCh)
			sock.CloseNow()

			wg.Wait()
			finalConencted.Close()

			return ctx.Err()
		}

		if _, err := finalConencted.Write([]byte("OK")); err != nil {
			return err
		}

		conn.UTPConn = finalConencted

		return nil
	}
}

func (conn *P2PConn) LocalDescription() (string, error) {
	if len(conn.LocalAddresses) == 0 {
		return "", ErrInsufficientLocalAdrdesses
	}

	conn.identifier = RandomSecureIdentifier()

	b, _ := json.Marshal(Description{
		LocalAddresses: conn.LocalAddresses,
		Identifier:     conn.identifier,
	})

	return string(b), nil
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
