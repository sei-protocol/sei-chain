package evmrpc

import (
	"context"
	"errors"
	"io"

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
	return &RateLimitGate{
		registry:      registry,
		parser:        ratelimiter.NewMethodParser(probeBytes),
		maxProbeBytes: probeBytes,
		enabled:       enabled,
		plane:         plane,
	}
}

// Check parses body for JSON-RPC method names and applies per-IP rate limits.
// Returns passthrough=true when the probe budget was exhausted before finding a
// method (ErrProbeLimit) — callers should admit without a rate-limit decision.
// rejectMethod is the method that exhausted the bucket when allowed=false.
func (g *RateLimitGate) Check(ctx context.Context, ip string, body io.Reader) (allowed bool, rejectMethod string, passthrough bool, err error) {
	if !g.enabled {
		return true, "", false, nil
	}

	methods, _, parseErr := g.parser.Parse(body)
	switch {
	case errors.Is(parseErr, ratelimiter.ErrProbeLimit):
		return true, "", true, nil
	case parseErr != nil:
		return false, "", false, parseErr
	}

	for _, method := range methods {
		if !g.registry.Allow(ctx, ip, g.plane, method) {
			return false, method, false, nil
		}
	}
	return true, "", false, nil
}
