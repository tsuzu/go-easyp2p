package easyp2p

import (
	"bytes"
	"context"
	"crypto/tls"
	"crypto/x509"
	"encoding/json"
	"errors"
	"io"
	"io/ioutil"
	"log"
	"net"
	"strings"
	"sync"
	"time"

	"github.com/lucas-clemente/quic-go"
)

type P2PConn struct {
	RawConn      *net.UDPConn
	ReliableConn net.Conn

	CertificateFunc    CertificateGeneratorType
	DiscoverIPTimeout  time.Duration
	DiscoverIPFunc     DiscoverIPFuncType
	IPDiscoveryServers []string
	LocalAddresses     []string

	identifier string
	cert       CertificatesType
}

func NewP2PConn(ipDiscoveryServers []string, discoverIP DiscoverIPFuncType) *P2PConn {
	return &P2PConn{
		IPDiscoveryServers: ipDiscoveryServers,
		DiscoverIPFunc:     discoverIP,
		DiscoverIPTimeout:  5 * time.Second,
	}
}

// If 0, ports will be automatically selected
func (conn *P2PConn) Listen(port int) (string, error) {
	udp, err := net.ListenUDP("udp", &net.UDPAddr{IP: net.IPv4zero, Port: port})

	if err != nil {
		return "", err
	}

	conn.RawConn = udp

	return udp.LocalAddr().String(), nil
}

// The first boolean value is whether one address at least is found  or not.
func (conn *P2PConn) DiscoverIP() (bool, error) {
	if conn.RawConn == nil {
		if _, err := conn.Listen(0); err != nil {
			return false, err
		}
	}

	var retErr []string
	leastOne := false

	addrs := make(map[string]struct{})

	if conn.DiscoverIPFunc != nil {
		for i := range conn.IPDiscoveryServers {
			server := conn.IPDiscoveryServers[i]

			addr, err := conn.DiscoverIPFunc(server, conn.RawConn, conn.DiscoverIPTimeout)

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
		localAddr := conn.RawConn.LocalAddr().String()
		port := localAddr[strings.LastIndex(localAddr, ":"):]

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

func (conn *P2PConn) Connect(remoteDescriptionString string, ctx context.Context) error {
	var remoteDescription Description
	if err := json.Unmarshal([]byte(remoteDescriptionString), &remoteDescription); err != nil {
		return err
	}

	if remoteDescription.ProtocolVersion != P2PVersionLatest {
		return ErrDifferentProtocol
	}

	pool := x509.NewCertPool()
	if !pool.AppendCertsFromPEM(remoteDescription.CAPEM) {
		return ErrInvalidCACertificate
	}

	asServer := bytes.Compare(remoteDescription.CAPEM, conn.cert.caCertPEM) > 0

	ignore := &PacketConnIgnoreOK{
		PacketConn: conn.RawConn,
	}

	if !asServer {
		ignore.addrs = make(map[string]struct{})

		for _, msg := range remoteDescription.LocalAddresses {
			ignore.addrs[msg] = struct{}{}
		}
		ignore.ch = make(chan string, 1024)
	}

	changeable := &PacketConnChangeableCloserStatus{PacketConn: ignore}

	if asServer {
		var config *tls.Config

		config = &tls.Config{
			Certificates: []tls.Certificate{conn.cert.cert},
			ClientCAs:    pool,
			ClientAuth:   tls.RequireAndVerifyClientCert,
		}

		quicConfig := &quic.Config{
			MaxIncomingStreams:    1,
			MaxIncomingUniStreams: 0,
			KeepAlive:             true,
		}

		filtered := newFilterPacketConn(changeable)

		listener, err := quic.Listen(filtered, config, quicConfig)

		if err != nil {
			return err
		}

		log.Println("start listening")

		go func() {
			for i := 0; i < 3; i++ {
				for i := range remoteDescription.LocalAddresses {
					addr, err := net.ResolveUDPAddr("udp", remoteDescription.LocalAddresses[i])
					if err == nil {
						changeable.WriteTo([]byte("OK"), addr)
					}
				}
			}
		}()

		finCh := make(chan struct{})
		var wg sync.WaitGroup
		var mut sync.RWMutex
		wg.Add(1)
		go func() {
			defer wg.Done()
			for {
				session, err := listener.Accept()

				select {
				case <-finCh:
					if session != nil {
						session.Close(nil)
					}
					return
				default:
				}

				if err != nil {
					log.Println(err)
					continue
				}

				log.Println("accepted")

				wg.Add(1)
				go func() {
					defer wg.Done()

					var accepted *quicSessionStream

					okChan := make(chan struct{})
					go func() {
						select {
						case <-okChan:
							return
						case <-finCh:
							select {
							case <-okChan:
							default:
								log.Println("close now")
								accepted.Close()
							}
						}
					}()

					accepted = newQuicSessionStream(session)

					ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
					if err := accepted.Init(true /*server mode*/, ctx); err != nil {
						log.Println(err)
						cancel()
						return
					}
					cancel()

					accepted.SetDeadline(time.Now().Add(5 * time.Second))

					log.Println("handshake start")

					var sendHeaderWG sync.WaitGroup
					sendHeaderWG.Add(1)
					go func() {
						defer sendHeaderWG.Done()
						// Send header
						hbytes := (&P2PHeader{
							Version: P2PVersionLatest,
						}).GetBytes()
						accepted.Write(hbytes[:])
					}()

					var hbytes [P2PHeaderLength]byte

					// Read header
					if _, err := accepted.Read(hbytes[:]); err != nil {
						close(okChan)
						accepted.Close()

						return
					}

					var header *P2PHeader
					if header, err = ParseP2PHeader(hbytes[:]); err != nil {
						close(okChan)
						accepted.Close()

						return
					}
					if header.Version != P2PVersionLatest {
						close(okChan)
						accepted.Close()

						return
					}

					sendHeaderWG.Wait()

					log.Println("a")

					b, err := ioutil.ReadAll(&io.LimitedReader{R: accepted, N: int64(IdentifierLength)})
					if err != nil {
						close(okChan)
						accepted.Close()

						return
					}
					if len(b) != IdentifierLength {
						close(okChan)
						accepted.Close()

						return
					}
					if string(b) != conn.identifier {
						close(okChan)
						accepted.Close()

						return
					}

					if _, err := accepted.Write([]byte(remoteDescription.Identifier)); err != nil {
						close(okChan)
						accepted.Close()

						return
					}

					b = make([]byte, 2)
					n, err := accepted.Read(b)

					if err != nil {
						close(okChan)
						accepted.Close()

						return
					}

					b = b[:n]
					accepted.SetDeadline(time.Time{})

					if string(b) == "OK" {
						mut.Lock()
						conn.ReliableConn = accepted
						mut.Unlock()

						close(okChan)
						close(finCh)
						log.Println("selected")
					} else {
						close(okChan)
						accepted.Close()
					}
				}()
			}
		}()
		select {
		case <-finCh:
			filtered.setAllowedAddress(conn.ReliableConn.RemoteAddr().String())
			changeable.SetStatus(true)
			wg.Wait()
			log.Println("waited")

		case <-ctx.Done():
			listener.Close()
			wg.Wait()
			if conn.ReliableConn != nil {
				conn.ReliableConn.Close()
			}

			return ctx.Err()
		}

		return nil
	} else {
		finCh := make(chan struct{})
		triggerCh := make(chan struct{})

		var selected net.Conn
		var mut sync.Mutex

		config := &tls.Config{
			RootCAs:      pool,
			ServerName:   CertificateServerName,
			Certificates: []tls.Certificate{conn.cert.cert},
		}

		sharable := newSharablePacketConn(changeable)

		sharable.Start()
		defer sharable.Stop()

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
				case addr = <-ignore.ch:
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
						finish := func() bool {
							cpc, err := sharable.Register(daddr)

							if err != nil {
								return true
							}

							log.Println("dial", daddr)
							session, err := quic.Dial(
								cpc,
								cpc.addr,
								CertificateServerName,
								config,
								&quic.Config{
									MaxIncomingStreams:    0,
									MaxIncomingUniStreams: 0,
									KeepAlive:             true,
								},
							)

							log.Println("connected", err)

							if err != nil {
								cpc.Close()

								return false
							}

							connected := newQuicSessionStream(session)

							ctx, cancel := context.WithTimeout(pctx, 5*time.Second)
							defer cancel()

							if err := connected.Init(false /*client mode*/, ctx); err != nil {
								return false
							}

							okChan := make(chan struct{})
							go func() {
								select {
								case <-okChan:
									return
								case <-finCh:
									select {
									case <-okChan:
									default:
										connected.SetDeadline(time.Now())
										connected.Close()
									}
								}
							}()

							connected.SetDeadline(time.Now().Add(5 * time.Second))

							var sendHeaderWG sync.WaitGroup
							sendHeaderWG.Add(1)
							go func() {
								defer sendHeaderWG.Done()
								// Send header
								hbytes := (&P2PHeader{
									Version: P2PVersionLatest,
								}).GetBytes()
								connected.Write(hbytes[:])
							}()

							// Read header
							var hbytes [P2PHeaderLength]byte
							if _, err := connected.Read(hbytes[:]); err != nil {
								close(okChan)
								connected.Close()

								return false
							}

							var header *P2PHeader
							if header, err = ParseP2PHeader(hbytes[:]); err != nil {
								close(okChan)
								connected.Close()

								return false
							}
							if header.Version != P2PVersionLatest {
								close(okChan)
								connected.Close()

								return false
							}

							sendHeaderWG.Wait()

							if _, err := connected.Write([]byte(remoteDescription.Identifier)); err != nil {
								close(okChan)
								connected.Close()

								return false
							}

							connected.SetDeadline(time.Now().Add(5 * time.Second))

							b, err := ioutil.ReadAll(&io.LimitedReader{R: connected, N: int64(IdentifierLength)})
							if err != nil {
								close(okChan)
								connected.Close()

								return false
							}

							if len(b) != IdentifierLength {
								close(okChan)
								connected.Close()

								return false
							}
							if string(b) != conn.identifier {
								close(okChan)
								connected.Close()

								return false
							}

							connected.SetDeadline(time.Time{})

							mut.Lock()
							if selected == nil {
								selected = connected

								close(okChan)
								close(triggerCh)
							} else {
								connected.Close()
							}
							mut.Unlock()
							return true
						}()

						if finish {
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

			wg.Wait()
			if selected != nil {
				selected.Close()
			}

			return ctx.Err()
		}

		if _, err := selected.Write([]byte("OK")); err != nil {
			return err
		}

		conn.ReliableConn = selected
		sharable.SwitchToOne(selected.RemoteAddr().String())
		changeable.SetStatus(true)
		log.Println("select")
		sharable.Stop()
		wg.Wait()
		log.Println("select ok")

		return nil
	}
}

func (conn *P2PConn) LocalDescription() (string, error) {
	if len(conn.LocalAddresses) == 0 {
		return "", ErrInsufficientLocalAdrdesses
	}

	conn.identifier = RandomSecureIdentifier()

	if conn.CertificateFunc == nil {
		conn.CertificateFunc = NewSelfSignedCertificate
	}

	cert, err := conn.CertificateFunc()

	if err != nil {
		return "", err
	}

	conn.cert = cert

	b, _ := json.Marshal(Description{
		ProtocolVersion: P2PVersionLatest,
		LocalAddresses:  conn.LocalAddresses,
		Identifier:      conn.identifier,
		CAPEM:           cert.caCertPEM,
	})

	return string(b), nil
}

func (conn *P2PConn) Read(b []byte) (int, error) {
	if conn.ReliableConn == nil {
		return 0, ErrNotConnected
	}

	return conn.ReliableConn.Read(b)
}

func (conn *P2PConn) Write(b []byte) (int, error) {
	if conn.ReliableConn == nil {
		return 0, ErrNotConnected
	}

	return conn.ReliableConn.Write(b)
}

func (conn *P2PConn) Close() error {
	if conn.ReliableConn != nil {
		e := conn.ReliableConn.Close()
		conn.ReliableConn = nil
		conn.RawConn = nil

		return e
	} else if conn.RawConn != nil {
		e := conn.RawConn.Close()
		conn.RawConn = nil

		return e
	}

	return nil
}
