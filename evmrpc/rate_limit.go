package evmrpc

import (
	"context"
	"errors"
	"io"
	"math"

	"github.com/sei-protocol/sei-chain/ratelimiter"
)

// RateLimitGate applies per-IP token-bucket rate limiting using a partial JSON
// read to extract JSON-RPC method names before full body decode.
type RateLimitGate struct {
	registry      *ratelimiter.Registry
	parser        *ratelimiter.MethodParser
	maxProbeBytes int64
	enabled       bool
	plane         string
}

// NewRateLimitGate returns a gate for the given plane ("evm"). registry must be non-nil.
func NewRateLimitGate(registry *ratelimiter.Registry, probeBytes int64, enabled bool, plane string) *RateLimitGate {
	if probeBytes <= 0 {
		probeBytes = ratelimiter.DefaultMaxProbeBytes
	}
	if probeBytes == math.MaxInt64 {
		probeBytes = math.MaxInt64 - 1
	}
	return &RateLimitGate{
		registry:      registry,
		parser:        ratelimiter.NewMethodParser(probeBytes),
		maxProbeBytes: probeBytes,
		enabled:       enabled,
		plane:         plane,
	}
}

// Check parses body for JSON-RPC method names and applies per-IP rate limits.
// rejectMethod is the method that exhausted the bucket when allowed=false.
func (g *RateLimitGate) Check(ctx context.Context, ip string, body io.Reader) (allowed bool, rejectMethod string, err error) {
	if !g.enabled {
		return true, "", nil
	}

	methods, _, parseErr := g.parser.Parse(body)
	switch {
	case errors.Is(parseErr, ratelimiter.ErrProbeLimit):
		// Body exceeded the probe budget without yielding a method (e.g. attacker
		// padded params ahead of method). Charge a token anyway so large bodies
		// can't dodge rate limiting by pushing method past the probe window.
		if !g.registry.Allow(ctx, ip, g.plane, "") {
			return false, "", nil
		}
		return true, "", nil
	case parseErr != nil:
		return false, "", parseErr
	}

	for _, method := range methods {
		if !g.registry.Allow(ctx, ip, g.plane, method) {
			return false, method, nil
		}
	}
	return true, "", nil
}
