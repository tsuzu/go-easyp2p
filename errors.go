package easyp2p

import (
	"errors"
	"net"
)

// ErrDisconnected is returned when the connection is closed or not established.
var ErrDisconnected = newError("Disconnected", false, false)

// ErrInsufficientLocalAdrdesses is returned when DiscoverIP() can find no addresses
var ErrInsufficientLocalAdrdesses = newError("Insufficient local addresses", false, false)

// ErrInvalidCACertificate is returned when the given CA certificate is invalid
var ErrInvalidCACertificate = newError("Invalid CA Certificate", false, false)

// ErrDifferentProtocol is returned when protocols don't match.
var ErrDifferentProtocol = newError("Different protocol", false, false)

// for sharablePacketConn

var errNotImplemented = newError("Not Implemented", false, false)
var errAlreadyRegistered = newError("Already Registered", false, true)
var errForbiddenAddress = newError("Forbidden Address", false, false)
var errSwitchedToOneAddress = newError("Switched to one address", false, false)
var errSwitchDirectConnectionMessage = errors.New("Switched to direct connection message")

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
