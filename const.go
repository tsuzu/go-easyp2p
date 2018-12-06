package easyp2p

import "time"

// InitialRetryingInterval is the interval after the first attempt to connect
const InitialRetryingInterval = 25 * time.Millisecond

// RetryingIntervalMultiplied : the interval is multiplied by this number as many times as we try to connect
const RetryingIntervalMultiplied = time.Duration(2)

type connectionMode bool

const (
	serverMode connectionMode = iota == 1
	clientMode
)
