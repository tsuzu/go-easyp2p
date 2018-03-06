package easyp2p

import (
	"errors"
	"net"
	"sync"
	"sync/atomic"
	"time"
)

var errNotImplemented = newError("Not Implemented", false, true)
var errAlreadyRegistered = newError("Already Registered", false, false)
var errForbiddenAddress = newError("Forbidden Address", false, true)
var errDisconnected = newError("Disconnected", false, true)
var errSwitchedToOneAddress = newError("Switched to one address", false, true)

var switchDirectConnectionMessage = errors.New("***swdc***")

type receivedValues struct {
	b   []byte
	err error
}

type registeredValue struct {
	recv []receivedValues
	wg   chan struct{}
}

type sharablePacketConn struct {
	pconn      net.PacketConn
	registered map[string]*registeredValue
	mut        sync.Mutex
	switchChan chan string
	stopChan   chan struct{}
	wg         sync.WaitGroup
}

func newSharablePacketConn(pconn net.PacketConn) *sharablePacketConn {
	return &sharablePacketConn{
		pconn:      pconn,
		registered: make(map[string]*registeredValue),
		switchChan: make(chan string, 1),
		stopChan:   make(chan struct{}),
	}
}

func (spc *sharablePacketConn) Start() {
	spc.wg.Add(1)
	go func() {
		defer spc.wg.Done()
		for {
			select {
			case addr := <-spc.switchChan:
				spc.mut.Lock()
				if rv, ok := spc.registered[addr]; ok {
					rv.recv = append(rv.recv, receivedValues{
						b:   nil,
						err: switchDirectConnectionMessage,
					})

					select {
					case rv.wg <- struct{}{}:
					default:
					}
				} else {
					spc.mut.Unlock()

					return
				}

				chs := make([]chan struct{}, 0, len(spc.registered))

				for k, v := range spc.registered {
					if k != addr {
						chs = append(chs, v.wg)
					}
				}

				spc.registered = map[string]*registeredValue{
					addr: spc.registered[addr],
				}

				for i := range chs {
					close(chs[i])
				}

				spc.mut.Unlock()

				return
			case <-spc.stopChan:
				spc.mut.Lock()

				chs := make([]chan struct{}, 0, len(spc.registered))

				for _, v := range spc.registered {
					chs = append(chs, v.wg)
				}

				spc.registered = map[string]*registeredValue{}

				for i := range chs {
					close(chs[i])
				}

				spc.mut.Unlock()
			default:
			}
			b := make([]byte, 65535)
			n, addr, err := spc.pconn.ReadFrom(b)

			if err != nil {
				if e, ok := err.(net.Error); ok {
					if !e.Temporary() {
						return
					}
				}
			}

			b = b[:n]
			spc.mut.Lock()

			if rv, ok := spc.registered[addr.String()]; ok {
				rv.recv = append(rv.recv, receivedValues{
					b:   b,
					err: err,
				})

				select {
				case rv.wg <- struct{}{}:
				default:
				}
			}
			spc.mut.Unlock()
		}
	}()
}

func (spc *sharablePacketConn) Register(addrStr string) (*childPacketConn, error) {
	spc.mut.Lock()
	defer spc.mut.Unlock()

	if _, ok := spc.registered[addrStr]; ok {
		return nil, errAlreadyRegistered
	}

	spc.registered[addrStr] = &registeredValue{
		recv: make([]receivedValues, 0, 32),
		wg:   make(chan struct{}, 1),
	}

	addr, err := net.ResolveUDPAddr("udp", addrStr)

	if err != nil {
		return nil, err
	}

	cpc := &childPacketConn{
		spc:  spc,
		addr: addr,
	}

	return cpc, nil
}

func (spc *sharablePacketConn) SwitchToOne(address string) {
	if spc.switchChan != nil {
		spc.switchChan <- address
		spc.wg.Wait()

		spc.stopChan = nil
		spc.switchChan = nil
	}
}

func (spc *sharablePacketConn) Stop() {
	if spc.stopChan != nil {
		close(spc.stopChan)
		spc.wg.Wait()

		spc.stopChan = nil
		spc.switchChan = nil
	}
}

type childPacketConn struct {
	spc  *sharablePacketConn
	addr net.Addr

	status int32 //atomic value 0: default 1: closed 2: switched
}

func (ipc *childPacketConn) ReadFrom(b []byte) (n int, addr net.Addr, err error) {
	for {
		switch atomic.LoadInt32(&ipc.status) {
		case 1:
			return 0, nil, errDisconnected
		case 2:
			for {
				n, addr, err = ipc.spc.pconn.ReadFrom(b)

				if addr == nil {
					return n, nil, err
				} else if addr.String() == ipc.addr.String() {

					return
				}
			}
		}

		ipc.spc.mut.Lock()
		rv, ok := ipc.spc.registered[ipc.addr.String()]

		if !ok {
			ipc.spc.mut.Unlock()

			return 0, nil, errDisconnected
		}
		if len(rv.recv) != 0 {
			current := rv.recv[0]
			rv.recv = rv.recv[1:]

			if current.err == switchDirectConnectionMessage && current.b == nil {
				atomic.StoreInt32(&ipc.status, 2)

				ipc.spc.mut.Unlock()
				continue
			}

			n = len(current.b)
			if len(b) < n {
				n = len(b)
			}
			copy(b, current.b[:n])

			ipc.spc.mut.Unlock()
			return n, ipc.addr, current.err
		}

		ipc.spc.mut.Unlock()

		<-rv.wg

	}
}

func (ipc *childPacketConn) WriteTo(b []byte, addr net.Addr) (n int, err error) {
	if addr.String() != ipc.addr.String() {
		return 0, errForbiddenAddress
	}

	if atomic.LoadInt32(&ipc.status) == 1 {
		return 0, errDisconnected
	}

	return ipc.spc.pconn.WriteTo(b, addr)
}

// Close closes the connection.
// Any blocked ReadFrom or WriteTo operations will be unblocked and return errors.
func (ipc *childPacketConn) Close() error {
	ipc.spc.mut.Lock()
	addr := ipc.addr.String()
	if rv, ok := ipc.spc.registered[addr]; ok {
		delete(ipc.spc.registered, addr)

		close(rv.wg)
	}
	ipc.spc.mut.Unlock()

	if atomic.LoadInt32(&ipc.status) == 2 {
		return ipc.spc.pconn.Close()
	}

	atomic.StoreInt32(&ipc.status, 1)

	return nil
}

func (ipc *childPacketConn) LocalAddr() net.Addr {
	return ipc.spc.pconn.LocalAddr()
}

func (ipc *childPacketConn) SetDeadline(t time.Time) error      { return errNotImplemented }
func (ipc *childPacketConn) SetReadDeadline(t time.Time) error  { return errNotImplemented }
func (ipc *childPacketConn) SetWriteDeadline(t time.Time) error { return errNotImplemented }
