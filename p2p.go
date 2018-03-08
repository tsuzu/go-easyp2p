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

// P2PConn is the  type of P2P connection
type P2PConn struct {
	RawConn      *net.UDPConn
	ReliableConn net.Conn

	CertificateFunc    CertificateGeneratorType
	IPDiscoveryTimeout time.Duration
	IPDiscoveryServers []string
	LocalAddresses     []string

	Identifier string
	cert       CertificatesType
	listener   quic.Listener
}

// NewP2PConn returns a new P2PConn
func NewP2PConn(ipDiscoveryServers []string) *P2PConn {
	return &P2PConn{
		IPDiscoveryServers: ipDiscoveryServers,
		IPDiscoveryTimeout: 5 * time.Second,
	}
}

// Listen opens a port and returns the local address.  If 0, ports will be automatically selected
func (conn *P2PConn) Listen(port int) (string, error) {
	udp, err := net.ListenUDP("udp", &net.UDPAddr{IP: net.IPv4zero, Port: port})

	if err != nil {
		return "", err
	}

	conn.RawConn = udp

	return udp.LocalAddr().String(), nil
}

// DiscoverIP finds local addresses from STUN servers and network interfaces
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

	for i := range conn.IPDiscoveryServers {
		server := conn.IPDiscoveryServers[i]

		addr, err := discoverIPAddressesWithSTUN(server, conn.RawConn, conn.IPDiscoveryTimeout)

		if err != nil {
			retErr = append(retErr, err.Error())

			continue
		}

		addrs[addr] = struct{}{}
		leastOne = true
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

// Connect tries to connect to the peer.
// remoteDescription needs to be given from the other peer
func (conn *P2PConn) Connect(ctx context.Context, remoteDescriptionString string) error {
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

	asServer := bytes.Compare(remoteDescription.CAPEM, conn.cert.CACertPEM) > 0

	if asServer {
		changeable := &packetConnChangeableValidness{PacketConn: conn.RawConn}

		changeable.SetStatus(validnessCloserDisabled)
		return conn.connectToClient(ctx, changeable, pool, remoteDescription)
	}

	ignore := &packetConnIgnoreOK{
		PacketConn: conn.RawConn,
	}
	changeable := &packetConnChangeableValidness{PacketConn: ignore}
	changeable.SetStatus(validnessCloserDisabled)

	ignore.addrs = make(map[string]struct{})

	for _, msg := range remoteDescription.LocalAddresses {
		ignore.addrs[msg] = struct{}{}
	}

	ignore.ch = make(chan string, 1024)

	return conn.connectToServer(ctx, ignore, changeable, pool, remoteDescription)
}

// LocalDescription returns a local description which needs to be passed to the other peer
func (conn *P2PConn) LocalDescription() (string, error) {
	if len(conn.LocalAddresses) == 0 {
		return "", ErrInsufficientLocalAdrdesses
	}

	conn.Identifier = randomSecureIdentifier()

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
		Identifier:      conn.Identifier,
		CAPEM:           cert.CACertPEM,
	})

	return string(b), nil
}

// Read reads data from the connection.
// Read can be made to time out and return an Error with Timeout() == true
// after a fixed time limit; see SetDeadline and SetReadDeadline.
func (conn *P2PConn) Read(b []byte) (int, error) {
	if conn.ReliableConn == nil {
		return 0, ErrDisconnected
	}

	return conn.ReliableConn.Read(b)
}

// Write writes data to the connection.
// Write can be made to time out and return an Error with Timeout() == true
// after a fixed time limit; see SetDeadline and SetWriteDeadline.
func (conn *P2PConn) Write(b []byte) (int, error) {
	if conn.ReliableConn == nil {
		return 0, ErrDisconnected
	}

	return conn.ReliableConn.Write(b)
}

// Close closes the connection. (sync)
// This will block until closing connections completes.
func (conn *P2PConn) Close() error {
	if conn.ReliableConn != nil {
		e := conn.ReliableConn.Close()

		if conn.listener != nil {
			conn.listener.Close()
		}

		return e
	} else if conn.RawConn != nil {
		e := conn.RawConn.Close()

		return e
	}

	return nil
}

// LocalAddr returns the local network address
func (conn *P2PConn) LocalAddr() net.Addr {
	if conn.ReliableConn == nil {
		return nil
	}

	return conn.ReliableConn.LocalAddr()
}

// RemoteAddr returns the remote network address
func (conn *P2PConn) RemoteAddr() net.Addr {
	if conn.ReliableConn == nil {
		return nil
	}

	return conn.ReliableConn.RemoteAddr()
}

// SetDeadline sets the read and write deadlines associated
// with the connection. It is equivalent to calling both
// SetReadDeadline and SetWriteDeadline.
//
// A deadline is an absolute time after which I/O operations
// fail with a timeout (see type Error) instead of
// blocking. The deadline applies to all future and pending
// I/O, not just the immediately following call to Read or
// Write. After a deadline has been exceeded, the connection
// can be refreshed by setting a deadline in the future.
//
// An idle timeout can be implemented by repeatedly extending
// the deadline after successful Read or Write calls.
//
// A zero value for t means I/O operations will not time out.
func (conn *P2PConn) SetDeadline(t time.Time) error {
	if conn.ReliableConn == nil {
		return ErrDisconnected
	}

	return conn.ReliableConn.SetDeadline(t)
}

// SetReadDeadline sets the deadline for future Read calls
// and any currently-blocked Read call.
// A zero value for t means Read will not time out.
func (conn *P2PConn) SetReadDeadline(t time.Time) error {
	if conn.ReliableConn == nil {
		return ErrDisconnected
	}

	return conn.ReliableConn.SetReadDeadline(t)
}

// SetWriteDeadline sets the deadline for future Write calls
// and any currently-blocked Write call.
// Even if write times out, it may return n > 0, indicating that
// some of the data was successfully written.
// A zero value for t means Write will not time out.
func (conn *P2PConn) SetWriteDeadline(t time.Time) error {
	if conn.ReliableConn == nil {
		return ErrDisconnected
	}

	return conn.ReliableConn.SetWriteDeadline(t)
}

func (conn *P2PConn) connectToServer(ctx context.Context, ignore *packetConnIgnoreOK, changeable *packetConnChangeableValidness, pool *x509.CertPool, remoteDescription Description) error {
	finCh := make(chan struct{})
	triggerCh := make(chan struct{})

	var selected net.Conn
	var mut sync.Mutex

	config := &tls.Config{
		RootCAs:      pool,
		ServerName:   CertificateServerName,
		Certificates: []tls.Certificate{conn.cert.Cert},
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

				retryingInterval := InitialRetryingInterval
			FINISH_TRYING:
				for {
					finish := func() bool {
						okChan := make(chan struct{})

						cpc, err := sharable.Register(daddr)

						if err != nil {
							return true
						}

						session, err := quic.Dial(
							cpc,
							cpc.addr,
							CertificateServerName,
							config,
							&quic.Config{
								MaxIncomingStreams:    0,
								MaxIncomingUniStreams: 0,
								HandshakeTimeout:      5 * time.Second,
								KeepAlive:             true,
							},
						)

						if err != nil {
							cpc.Close()

							return false
						}

						go func() {
							select {
							case <-okChan:
								return
							case <-finCh:
								select {
								case <-okChan:
								default:
									session.Close(nil)
								}
							}
						}()

						connected := newquicSessionStream(session)

						ctx, cancel := context.WithTimeout(pctx, 5*time.Second)
						defer cancel()

						if err := connected.init(ctx, false /*client mode*/); err != nil {
							connected.Close()

							return false
						}

						connected.SetDeadline(time.Now().Add(5 * time.Second))

						var sendHeaderWG sync.WaitGroup
						sendHeaderWG.Add(1)
						go func() {
							defer sendHeaderWG.Done()
							// Send header
							hbytes := (&p2pHeader{
								Version: P2PVersionLatest,
							}).GetBytes()
							connected.Write(hbytes[:])
						}()

						// Read header
						var hbytes [p2pHeaderLength]byte
						if _, err := connected.Read(hbytes[:]); err != nil {
							close(okChan)
							connected.Close()

							return false
						}

						var header *p2pHeader
						if header, err = parseP2PHeader(hbytes[:]); err != nil {
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

						b, err := ioutil.ReadAll(&io.LimitedReader{R: connected, N: int64(identifierLength)})
						if err != nil {
							close(okChan)
							connected.Close()

							return false
						}

						if len(b) != identifierLength {
							close(okChan)
							connected.Close()

							return false
						}
						if string(b) != conn.Identifier {
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
					timer := time.NewTimer(retryingInterval)

					select {
					case <-timer.C:

					case <-finCh:
						break FINISH_TRYING
					}

					timer.Stop()

					retryingInterval *= RetryingIntervalMultiplied
				}
			}(addr)
		}
	}()

	select {
	case <-triggerCh:
		close(finCh)

	case <-ctx.Done():
		close(finCh)
		changeable.SetDeadline(time.Now().Add(-10 * time.Second))
		changeable.SetStatus(validnessDisabled)

		wg.Wait()
		mut.Lock()
		if selected != nil {
			selected.Close()
		}
		mut.Unlock()

		return ctx.Err()
	}

	if _, err := selected.Write([]byte("OK")); err != nil {
		return err
	}

	conn.ReliableConn = selected
	sharable.SwitchToOne(selected.RemoteAddr().String())
	changeable.SetStatus(validnessEnabled)
	wg.Wait()

	return nil
}

func (conn *P2PConn) connectToClient(ctx context.Context, changeable *packetConnChangeableValidness, pool *x509.CertPool, remoteDescription Description) error {
	acceptCancelCert, err := conn.CertificateFunc()

	if err != nil {
		return err
	}

	if !pool.AppendCertsFromPEM(acceptCancelCert.CACertPEM) {
		return ErrInvalidCACertificate
	}

	config := &tls.Config{
		Certificates: []tls.Certificate{conn.cert.Cert},
		ClientCAs:    pool,
		ClientAuth:   tls.RequireAndVerifyClientCert,
	}

	quicConfig := &quic.Config{
		MaxIncomingStreams:    1,
		MaxIncomingUniStreams: 0,
		KeepAlive:             true,
		IdleTimeout:           10 * time.Second,
	}

	filtered := newFilterPacketConn(changeable)

	listener, err := quic.Listen(filtered, config, quicConfig)

	if err != nil {
		return err
	}

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

		var wgAccepted sync.WaitGroup
		defer wgAccepted.Wait()

		acceptChan := make(chan quic.Session, 10)
		go func() {
			defer close(acceptChan)
			for {
				session, err := listener.Accept()

				if err != nil {
					return
				}

				select {
				case <-finCh:
					session.Close(nil)
					return
				default:
				}

				acceptChan <- session
			}
		}()

		for {
			var session quic.Session
			var ok bool
			select {
			case session, ok = <-acceptChan:
				if !ok {
					return
				}
			case <-finCh:
				if session != nil {
					session.Close(nil)
				}
				return
			}

			if err != nil {
				continue
			}

			wgAccepted.Add(1)
			go func() {
				defer wgAccepted.Done()

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
							accepted.Close()
						}
					}
				}()

				accepted = newquicSessionStream(session)

				ictx, cancel := context.WithTimeout(ctx, 3*time.Second)
				if err := accepted.init(ictx, true /*server mode*/); err != nil {
					cancel()

					accepted.CloseNow()
					return
				}
				cancel()

				accepted.SetDeadline(time.Now().Add(5 * time.Second))

				var sendHeaderWG sync.WaitGroup
				sendHeaderWG.Add(1)
				go func() {
					defer sendHeaderWG.Done()
					// Send header
					hbytes := (&p2pHeader{
						Version: P2PVersionLatest,
					}).GetBytes()
					accepted.Write(hbytes[:])
				}()

				var hbytes [p2pHeaderLength]byte

				// Read header
				if _, err := accepted.Read(hbytes[:]); err != nil {
					close(okChan)
					accepted.CloseNow()

					return
				}

				var header *p2pHeader
				if header, err = parseP2PHeader(hbytes[:]); err != nil {
					close(okChan)
					accepted.CloseNow()

					return
				}
				if header.Version != P2PVersionLatest {
					close(okChan)
					accepted.CloseNow()

					return
				}

				sendHeaderWG.Wait()

				b, err := ioutil.ReadAll(&io.LimitedReader{R: accepted, N: int64(identifierLength)})
				if err != nil {
					close(okChan)
					accepted.CloseNow()

					return
				}
				if len(b) != identifierLength {
					close(okChan)
					accepted.CloseNow()

					return
				}
				if string(b) != conn.Identifier {
					close(okChan)
					accepted.CloseNow()

					return
				}

				if _, err := accepted.Write([]byte(remoteDescription.Identifier)); err != nil {
					close(okChan)
					accepted.CloseNow()

					return
				}

				b = make([]byte, 2)
				n, err := accepted.Read(b)

				if err != nil {
					close(okChan)
					accepted.CloseNow()

					return
				}

				b = b[:n]
				accepted.SetDeadline(time.Time{})

				if string(b) == "OK" {
					mut.Lock()

					if conn.ReliableConn == nil {
						conn.ReliableConn = accepted

						close(okChan)
						close(finCh)
					}
					mut.Unlock()

				} else {
					close(okChan)
					accepted.CloseNow()
				}
			}()
		}
	}()

	stopAccepting := func(localaddr, allowed string) {
		// Stop accepting TODO: change into a smarter way
		go func() {
			defer filtered.setAllowedAddress(allowed)
			addr := localaddr
			port := addr[strings.LastIndex(addr, ":")+1:]

			c, err := quic.DialAddr(
				"localhost:"+port,
				&tls.Config{
					Certificates:       []tls.Certificate{acceptCancelCert.Cert},
					InsecureSkipVerify: true,
				},
				&quic.Config{
					MaxIncomingStreams:    0,
					MaxIncomingUniStreams: 0,
					HandshakeTimeout:      1 * time.Second,
					IdleTimeout:           1 * time.Second,
				},
			)

			if err == nil {
				c.Close(nil)
			} else {
				log.Println(err)
			}
		}()
	}

	select {
	case <-finCh:
		stopAccepting(listener.Addr().String(), conn.ReliableConn.RemoteAddr().String())

		wg.Wait()
		changeable.SetStatus(validnessEnabled)
		conn.listener = listener

	case <-ctx.Done():
		changeable.SetDeadline(time.Now().Add(-10 * time.Second))
		changeable.SetStatus(validnessDisabled)
		listener.Close()
		wg.Wait()

		mut.Lock()
		if conn.ReliableConn != nil {
			conn.ReliableConn.Close()
			conn.ReliableConn = nil
		}
		mut.Unlock()

		return ctx.Err()
	}

	return nil
}
