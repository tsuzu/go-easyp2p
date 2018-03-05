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
}

func newQuicSessionStream(session quic.Session) *quicSessionStream {
	return &quicSessionStream{
		session: session,
	}
}

// mode: true->server false->client
func (qss *quicSessionStream) Init(mode bool, ctx context.Context) error {
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

func (qss *quicSessionStream) Read(b []byte) (n int, err error) {
	return qss.stream.Read(b)
}

func (qss *quicSessionStream) Write(b []byte) (n int, err error) {
	return qss.stream.Write(b)
}

func (qss *quicSessionStream) Close() error {
	if qss.stream != nil {
		qss.stream.Close()
	}
	return qss.session.Close(nil)
}

func (qss *quicSessionStream) LocalAddr() net.Addr {
	return qss.session.LocalAddr()
}

func (qss *quicSessionStream) RemoteAddr() net.Addr {
	return qss.session.RemoteAddr()
}

func (qss *quicSessionStream) SetDeadline(t time.Time) error {
	return qss.stream.SetDeadline(t)
}

func (qss *quicSessionStream) SetReadDeadline(t time.Time) error {
	return qss.stream.SetReadDeadline(t)
}

func (qss *quicSessionStream) SetWriteDeadline(t time.Time) error {
	return qss.stream.SetWriteDeadline(t)
}
