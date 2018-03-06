package easyp2p

import (
	"errors"
	"net"
)

// ErrNotConnected is returned when packets are sent/received while connection is not connected.
var ErrNotConnected = newError("Not connected", false, true)

// ErrInsufficientLocalAdrdesses is returned when DiscoverIP() can find no addresses
var ErrInsufficientLocalAdrdesses = newError("Insufficient local addresses", false, true)

// ErrInvalidCACertificate is returned when given CA certificates is invalid
var ErrInvalidCACertificate = newError("Invalid CA Certificate", false, true)

// ErrDifferentProtocol is returned when protocols don't match.
var ErrDifferentProtocol = newError("Different protocol", false, true)

func newError(msg string, timeout, temporary bool) net.Error {
	return &NetError{
		errors.New(msg),
		timeout, temporary,
	}
}

// NetError is the error type of net.Error
type NetError struct {
	error
	timeout, temporary bool
}

// Timeout (refer to net.Error)
func (ne *NetError) Timeout() bool {
	return ne.timeout
}

// Temporary (refer to net.Error)
func (ne *NetError) Temporary() bool {
	return ne.temporary
}
