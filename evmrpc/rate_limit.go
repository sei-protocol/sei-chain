package evmrpc

import (
	"context"
	"io"
	"math"

	"github.com/sei-protocol/sei-chain/ratelimiter"
)

// RateLimitGate applies per-IP token-bucket rate limiting after extracting JSON-RPC
// method names from the entire request body (bounded by max_request_body_bytes).
// When enabled, method extraction and fail-closed rejection (HTTP 400/413) run even
// if the registry is configured with RPS or burst zero (no HTTP 429 only).
type RateLimitGate struct {
	registry     *ratelimiter.Registry
	parser       *ratelimiter.MethodParser
	maxBodyBytes int64
	enabled      bool
	plane        string
}

// NewRateLimitGate returns a gate for the given plane ("evm"). registry must be non-nil.
// maxBodyBytes is the same cap as max_request_body_bytes; non-positive values use
// defaultMaxRequestBodyBytes.
func NewRateLimitGate(registry *ratelimiter.Registry, maxBodyBytes int64, enabled bool, plane string) *RateLimitGate {
	if maxBodyBytes <= 0 {
		maxBodyBytes = defaultMaxRequestBodyBytes
	}
	if maxBodyBytes == math.MaxInt64 {
		maxBodyBytes = math.MaxInt64 - 1
	}
	return &RateLimitGate{
		registry:     registry,
		parser:       ratelimiter.NewMethodParser(maxBodyBytes),
		maxBodyBytes: maxBodyBytes,
		enabled:      enabled,
		plane:        plane,
	}
}

// Check parses body for JSON-RPC method names and applies per-IP rate limits.
// rejectMethod is the method that exhausted the bucket when allowed=false.
// Any Parse error, including ErrProbeLimit, is returned to the caller for rejection.
func (g *RateLimitGate) Check(ctx context.Context, ip string, body io.Reader) (allowed bool, rejectMethod string, err error) {
	if !g.enabled {
		return true, "", nil
	}

	methods, _, parseErr := g.parser.Parse(body)
	if parseErr != nil {
		return false, "", parseErr
	}

	for _, method := range methods {
		if !g.registry.Allow(ctx, ip, g.plane, method) {
			return false, method, nil
		}
	}
	return true, "", nil
}
