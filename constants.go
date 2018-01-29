package easyp2p

import "errors"

const IPDiscoveryVersion = "1.0"
const IPDiscoveryRequestHeader = "DISCOVER_IP"
const IPDiscoveryResponseHeaderOK = "OK"
const IPDiscoveryResponseHeaderProcotolError = "PROTOCOL_ERROR"

var ErrUnknownProtocol = errors.New("Unknown DiscoverIP Protocol")
var ErrNotConnected = errors.New("Not connected")
var ErrInsufficientLocalAdrdesses = errors.New("Insufficient local addresses")
