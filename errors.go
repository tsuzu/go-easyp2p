package easyp2p

import (
	"errors"
	"net"
)

// ErrDisconnected is returned when the connection is closed or not established.
var ErrDisconnected = newError("Disconnected", false, true)

// ErrInsufficientLocalAdrdesses is returned when DiscoverIP() can find no addresses
var ErrInsufficientLocalAdrdesses = newError("Insufficient local addresses", false, true)

// ErrInvalidCACertificate is returned when given CA certificates is invalid
var ErrInvalidCACertificate = newError("Invalid CA Certificate", false, true)

// ErrDifferentProtocol is returned when protocols don't match.
var ErrDifferentProtocol = newError("Different protocol", false, true)

// for sharablePacketConn

var errNotImplemented = newError("Not Implemented", false, true)
var errAlreadyRegistered = newError("Already Registered", false, false)
var errForbiddenAddress = newError("Forbidden Address", false, true)
var errSwitchedToOneAddress = newError("Switched to one address", false, true)
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
