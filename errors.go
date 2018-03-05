package easyp2p

import (
	"errors"
	"net"
)

func newError(msg string, timeout, temporary bool) net.Error {
	return &NetError{
		errors.New(msg),
		timeout, temporary,
	}
}

type NetError struct {
	error
	timeout, temporary bool
}

func (ne *NetError) Timeout() bool {
	return ne.timeout
}
func (ne *NetError) Temporary() bool {
	return ne.temporary
}
