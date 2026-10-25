package rpc

import (
	"errors"
	"sync"
)

var (
	errNewHeadsSubscriptionLimit   = errors.New("no new newHeads subscription can be created: the node's limit is reached")
	errConnectionSubscriptionLimit = errors.New("no new subscription can be created: this connection's limit is reached")
)

// subscriptionLimiter counts open newHeads subscriptions against a node-wide
// cap and each connection's subscriptions against a per-connection cap. A zero
// cap disables that limit.
type subscriptionLimiter struct {
	maxNewHeads      uint64
	maxPerConnection uint64

	mu            sync.Mutex
	newHeads      uint64
	perConnection map[string]uint64
}

func newSubscriptionLimiter(maxNewHeads, maxPerConnection uint64) *subscriptionLimiter {
	return &subscriptionLimiter{
		maxNewHeads:      maxNewHeads,
		maxPerConnection: maxPerConnection,
		perConnection:    make(map[string]uint64),
	}
}

// acquireNewHeads reserves a newHeads subscription slot on connection, or
// returns an error when either cap is reached. Each successful call must be
// matched by exactly one releaseNewHeads for the same connection.
func (l *subscriptionLimiter) acquireNewHeads(connection string) error {
	l.mu.Lock()
	defer l.mu.Unlock()
	if l.maxNewHeads > 0 && l.newHeads >= l.maxNewHeads {
		return errNewHeadsSubscriptionLimit
	}
	if l.maxPerConnection > 0 && l.perConnection[connection] >= l.maxPerConnection {
		return errConnectionSubscriptionLimit
	}
	l.newHeads++
	l.perConnection[connection]++
	return nil
}

// releaseNewHeads frees a slot acquireNewHeads reserved on connection.
func (l *subscriptionLimiter) releaseNewHeads(connection string) {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.newHeads--
	l.perConnection[connection]--
	if l.perConnection[connection] == 0 {
		delete(l.perConnection, connection)
	}
}
