package easyp2p

import "time"

// InitialRetryingInterval is the interval after the first attempt to connect
const InitialRetryingInterval = 25 * time.Millisecond

// RetryingIntervalMultipied : the interval is multiplied by this number as many times as we try to connect
const RetryingIntervalMultipied = time.Duration(2)
