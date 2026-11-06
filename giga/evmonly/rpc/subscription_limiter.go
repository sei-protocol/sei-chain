package rpc

import (
	"errors"
	"sync"
)

var (
	errNewHeadsSubscriptionLimit   = errors.New("no new newHeads subscription can be created: the node's limit is reached")
	errLogsSubscriptionLimit       = errors.New("no new logs subscription can be created: the node's limit is reached")
	errConnectionSubscriptionLimit = errors.New("no new subscription can be created: this connection's limit is reached")
)

// subscriptionLimiter counts open newHeads and logs subscriptions against node-wide caps and each
// connection's subscriptions against a per-connection cap. A zero cap disables that limit.
type subscriptionLimiter struct {
	maxNewHeads      uint64
	maxLogs          uint64
	maxPerConnection uint64

	mu            sync.Mutex
	newHeads      uint64
	logs          uint64
	perConnection map[string]uint64
}

func newSubscriptionLimiter(maxNewHeads, maxLogs, maxPerConnection uint64) *subscriptionLimiter {
	return &subscriptionLimiter{
		maxNewHeads:      maxNewHeads,
		maxLogs:          maxLogs,
		maxPerConnection: maxPerConnection,
		perConnection:    make(map[string]uint64),
	}
}

// acquireNewHeads reserves a newHeads subscription slot on connection, or
// returns an error when either cap is reached. Each successful call must be
// matched by exactly one releaseNewHeads for the same connection.
func (l *subscriptionLimiter) acquireNewHeads(connection string) error {
	return l.acquire(connection, &l.newHeads, l.maxNewHeads, errNewHeadsSubscriptionLimit)
}

// releaseNewHeads frees a slot acquireNewHeads reserved on connection.
func (l *subscriptionLimiter) releaseNewHeads(connection string) {
	l.release(connection, &l.newHeads)
}

// acquireLogs reserves a logs subscription slot on connection, or returns an error when either
// cap is reached. Each successful call must be matched by exactly one releaseLogs for the same
// connection.
func (l *subscriptionLimiter) acquireLogs(connection string) error {
	return l.acquire(connection, &l.logs, l.maxLogs, errLogsSubscriptionLimit)
}

// releaseLogs frees a slot acquireLogs reserved on connection.
func (l *subscriptionLimiter) releaseLogs(connection string) {
	l.release(connection, &l.logs)
}

func (l *subscriptionLimiter) acquire(connection string, open *uint64, maxOpen uint64, errLimit error) error {
	l.mu.Lock()
	defer l.mu.Unlock()
	if maxOpen > 0 && *open >= maxOpen {
		return errLimit
	}
	if l.maxPerConnection > 0 && l.perConnection[connection] >= l.maxPerConnection {
		return errConnectionSubscriptionLimit
	}
	*open++
	l.perConnection[connection]++
	return nil
}

func (l *subscriptionLimiter) release(connection string, open *uint64) {
	l.mu.Lock()
	defer l.mu.Unlock()
	*open--
	l.perConnection[connection]--
	if l.perConnection[connection] == 0 {
		delete(l.perConnection, connection)
	}
}
