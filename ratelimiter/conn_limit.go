package ratelimiter

import (
	"context"
	"net"
	"sync"

	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/metric"
)

// ConnLimitListener returns a listener that bounds the number of simultaneously
// open connections from any one client address to maxPerIP. A non-positive
// maxPerIP returns inner unchanged.
//
// It bounds the layer below the RPC — accepted sockets, TLS handshakes, HTTP/2
// frame state — which a per-RPC counter cannot see, and it caps the per-address
// share of the global connection budget. Wrap the raw listener with this before
// the global cap, so a connection this rejects never spends a global slot.
//
// Addresses are keyed the way rate-limit buckets are, so a client rotating
// within an IPv6 /64 does not get a fresh allowance per address, and every
// client sharing an address shares one allowance.
func ConnLimitListener(inner net.Listener, plane string, maxPerIP int) net.Listener {
	if maxPerIP <= 0 {
		return inner
	}
	return &connLimitListener{
		Listener: inner,
		plane:    plane,
		counter:  newInflightCounter(maxPerIP),
	}
}

// connLimitListener is the listener ConnLimitListener returns.
type connLimitListener struct {
	net.Listener

	plane   string
	counter *inflightCounter
}

// Accept returns the next connection whose client address is under the cap,
// closing and counting the ones that are not.
//
// Over-cap connections are dropped here rather than surfaced as an Accept error,
// because an Accept error stops the serving loop and would turn one client's
// excess into an outage for every other client.
func (l *connLimitListener) Accept() (net.Conn, error) {
	for {
		conn, err := l.Listener.Accept()
		if err != nil {
			return nil, err
		}
		key := bucketKey(stripPort(conn.RemoteAddr().String()))
		if l.counter.acquire(key) {
			return &limitedConn{Conn: conn, release: func() { l.counter.release(key) }}, nil
		}
		registryMetrics.connRejectedCounter.Add(
			context.Background(),
			1,
			metric.WithAttributes(attribute.String("plane", l.plane)),
		)
		_ = conn.Close()
	}
}

// limitedConn returns its address's connection slot when it is closed.
type limitedConn struct {
	net.Conn

	once    sync.Once
	release func()
}

// Close closes the underlying connection and returns its slot. It is safe to
// call more than once: net/http and grpc-go both close a connection from more
// than one path, and a double release would hand the address a free slot.
func (c *limitedConn) Close() error {
	err := c.Conn.Close()
	c.once.Do(c.release)
	return err
}
