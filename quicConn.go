package easyp2p

import (
	"context"
	"net"
	"sync"
	"time"

	"github.com/lucas-clemente/quic-go"
)

type quicSessionStream struct {
	session quic.Session
	stream  quic.Stream

	once      sync.Once
	acceptErr error
	closed    chan struct{}

	recvMutex sync.Mutex
	recv      []receivedValues // sharablePacketConn.go

	readDeadlineMut sync.Mutex
	readDeadline    time.Time
}

func newquicSessionStream(session quic.Session) *quicSessionStream {
	return &quicSessionStream{
		session: session,
		closed:  make(chan struct{}, 1),
		recv:    make([]receivedValues, 0, 32),
	}
}

// mode: true->server false->client
// this must be called only once
func (qss *quicSessionStream) init(ctx context.Context, mode bool) error {
	qss.once.Do(func() {
		fin := make(chan struct{}, 1)
		go func() {
			select {
			case <-ctx.Done():
				qss.session.Close(ctx.Err())
			case <-fin:
				return
			}
		}()

		var stream quic.Stream
		var err error
		if mode {
			stream, err = qss.session.AcceptStream()
		} else {
			stream, err = qss.session.OpenStream()
		}
		close(fin)

		qss.acceptErr = err
		qss.stream = stream
	})

	return qss.acceptErr
}

func (qss *quicSessionStream) read(b []byte) (n int, closed bool, err error) {
	n, err = qss.stream.Read(b)

	if err != nil {
		if err, ok := err.(net.Error); ok {
			if !(err.Timeout() || err.Temporary()) {
				select {
				case qss.closed <- struct{}{}:
				default:
				}

				closed = true
			}
		} else {
			select {
			case qss.closed <- struct{}{}:
			default:
			}

			closed = true
		}
	}

	return
}

func (qss *quicSessionStream) readWorker() (closed bool) {
	qss.readDeadlineMut.Lock()
	if (qss.readDeadline != time.Time{} && time.Now().Before(qss.readDeadline)) {
		qss.readDeadline = time.Now().Add(1 * time.Second)
		qss.stream.SetReadDeadline(qss.readDeadline)
	}
	qss.readDeadlineMut.Unlock()

	var n int
	var err error
	b := make([]byte, 32*1024)

	qss.recvMutex.Lock()
	defer qss.recvMutex.Unlock()

	n, closed, err = qss.read(b)

	b = b[:n]

	qss.recv = append(qss.recv, receivedValues{b, err})

	return
}

func (qss *quicSessionStream) Read(b []byte) (n int, err error) {
	qss.recvMutex.Lock()
	defer qss.recvMutex.Unlock()

	if len(qss.recv) != 0 {
		recv := qss.recv[0]
		qss.recv = qss.recv[1:]

		n = len(recv.b)
		if len(recv.b) > len(b) {
			n = len(b)
		}

		err = recv.err
	} else {
		n, _, err = qss.read(b)
	}
	return
}

func (qss *quicSessionStream) Write(b []byte) (n int, err error) {
	return qss.stream.Write(b)
}

func (qss *quicSessionStream) Close() error {
	if qss.stream != nil {
		if err := qss.stream.Close(); err != nil {
			return err
		}

		go func() {
			for !qss.readWorker() {
			}
		}()

		timer := time.NewTimer(10 * time.Second)

		select {
		case <-timer.C:
		case <-qss.closed:
		}
		timer.Stop()

	}

	if qss.session != nil {
		if err := qss.session.Close(nil); err != nil {
			return err
		}
	}

	return nil
}

// CloseNow closes the connection abruptly
func (qss *quicSessionStream) CloseNow() {
	qss.session.Close(nil)
}

func (qss *quicSessionStream) LocalAddr() net.Addr {
	return qss.session.LocalAddr()
}

func (qss *quicSessionStream) RemoteAddr() net.Addr {
	return qss.session.RemoteAddr()
}

func (qss *quicSessionStream) SetDeadline(t time.Time) error {
	qss.SetReadDeadline(t)
	qss.SetWriteDeadline(t)

	return nil
}

func (qss *quicSessionStream) SetReadDeadline(t time.Time) error {
	qss.readDeadlineMut.Lock()
	qss.readDeadline = t
	err := qss.stream.SetReadDeadline(qss.readDeadline)
	qss.readDeadlineMut.Unlock()

	return err
}

func (qss *quicSessionStream) SetWriteDeadline(t time.Time) error {
	return qss.stream.SetWriteDeadline(t)
}
