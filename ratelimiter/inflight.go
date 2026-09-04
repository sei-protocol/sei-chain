package ratelimiter

import (
	"context"
	"sync"

	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/metric"
)

// inflightCounter bounds the number of slots held concurrently per key.
//
// A key is dropped once its count returns to zero, so the map is bounded by the
// number of slots actually held rather than by the number of addresses seen.
type inflightCounter struct {
	max int

	mu   sync.Mutex
	held map[string]int
}

func newInflightCounter(max int) *inflightCounter {
	return &inflightCounter{max: max, held: make(map[string]int)}
}

// acquire takes one slot for key and reports whether it was available.
func (c *inflightCounter) acquire(key string) bool {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.held[key] >= c.max {
		return false
	}
	c.held[key]++
	return true
}

// release returns one slot for key. Releasing a key holding none is a no-op.
func (c *inflightCounter) release(key string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	n, ok := c.held[key]
	if !ok {
		return
	}
	if n <= 1 {
		delete(c.held, key)
		return
	}
	c.held[key] = n - 1
}

// heldFor returns the number of slots key currently holds.
func (c *inflightCounter) heldFor(key string) int {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.held[key]
}

// AcquireInFlight takes one concurrency slot for ip and reports whether one was
// available. It returns true when the Registry has no in-flight limit, so a
// caller pairs it with ReleaseInFlight unconditionally.
//
// Rejections increment rpc_inflight_rejected_total{plane, method_namespace},
// the concurrency sibling of rpc_rate_limit_rejected_total.
func (r *Registry) AcquireInFlight(ctx context.Context, ip, plane, method string) bool {
	if r.inflight == nil {
		return true
	}
	if r.inflight.acquire(bucketKey(ip)) {
		return true
	}
	registryMetrics.inflightRejectedCounter.Add(
		ctx,
		1,
		metric.WithAttributes(
			attribute.String("plane", plane),
			attribute.String("method_namespace", bucketRPCMethod(plane, method, r.knownGRPCMethods())),
		),
	)
	return false
}

// ReleaseInFlight returns the concurrency slot AcquireInFlight took for ip.
func (r *Registry) ReleaseInFlight(ip string) {
	if r.inflight == nil {
		return
	}
	r.inflight.release(bucketKey(ip))
}

// InFlightHeld returns the number of concurrency slots ip currently holds, and 0
// when the Registry has no in-flight limit.
func (r *Registry) InFlightHeld(ip string) int {
	if r.inflight == nil {
		return 0
	}
	return r.inflight.heldFor(bucketKey(ip))
}

// IsKnownGRPCMethod reports whether fullMethod is one of the "service/Method"
// names given to SetKnownGRPCMethods, with or without a leading slash. It
// reports false when SetKnownGRPCMethods has not been called.
func (r *Registry) IsKnownGRPCMethod(fullMethod string) bool {
	known := r.knownGRPCMethods()
	if known == nil {
		return false
	}
	_, ok := known[trimLeadingSlash(fullMethod)]
	return ok
}
