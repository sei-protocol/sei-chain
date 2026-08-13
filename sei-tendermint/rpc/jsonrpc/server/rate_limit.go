package server

import (
	"context"
	"errors"
	"io"
	"math"
	"strings"

	"github.com/sei-protocol/sei-chain/ratelimiter"
)

const cometbftRateLimitPlane = "cometbft"

var errInvalidURIMethod = errors.New("invalid URI method")

// RateLimitGate applies per-IP token-bucket rate limiting for CometBFT RPC HTTP
// requests. POST JSON-RPC bodies are parsed with MethodParser before full decode;
// GET URI routes are accounted by path-derived method names.
type RateLimitGate struct {
	registry     *ratelimiter.Registry
	parser       *ratelimiter.MethodParser
	maxBodyBytes int64
	enabled      bool
	plane        string
}

// NewRateLimitGate returns a gate for CometBFT RPC HTTP (plane "cometbft").
// registry must be non-nil. maxBodyBytes should match max-body-bytes; non-positive
// values use DefaultConfig().MaxBodyBytes.
func NewRateLimitGate(registry *ratelimiter.Registry, maxBodyBytes int64, enabled bool) *RateLimitGate {
	if maxBodyBytes <= 0 {
		maxBodyBytes = DefaultConfig().MaxBodyBytes
	}
	if maxBodyBytes == math.MaxInt64 {
		maxBodyBytes = math.MaxInt64 - 1
	}
	return &RateLimitGate{
		registry:     registry,
		parser:       ratelimiter.NewMethodParser(maxBodyBytes),
		maxBodyBytes: maxBodyBytes,
		enabled:      enabled,
		plane:        cometbftRateLimitPlane,
	}
}

// chargeAdmissionRejection consumes one token for a fail-closed rejection that
// never reaches method parsing (oversize body, read error). Returns true when
// the bucket is exhausted and the caller should respond with HTTP 429.
func (g *RateLimitGate) chargeAdmissionRejection(ctx context.Context, ip string) bool {
	if !g.enabled {
		return false
	}
	return !g.registry.Allow(ctx, ip, g.plane, ratelimiter.MethodInvalid)
}

// CheckPOST parses body for JSON-RPC method names and applies per-IP rate limits.
// Parse errors still charge the bucket under ratelimiter.MethodInvalid so
// malformed bodies can't bypass rate limiting.
func (g *RateLimitGate) CheckPOST(ctx context.Context, ip string, body io.Reader) (allowed bool, rejectMethod string, err error) {
	if !g.enabled {
		return true, "", nil
	}

	methods, _, parseErr := g.parser.Parse(body)
	if parseErr != nil {
		if !g.registry.Allow(ctx, ip, g.plane, ratelimiter.MethodInvalid) {
			return false, ratelimiter.MethodInvalid, nil
		}
		return false, "", parseErr
	}

	if n := len(methods); n > 0 && !g.registry.AllowN(ctx, ip, g.plane, methods[0], n) {
		return false, methods[0], nil
	}
	return true, "", nil
}

// CheckURI applies per-IP rate limits for REST-style GET/HEAD RPC routes.
func (g *RateLimitGate) CheckURI(ctx context.Context, ip, path string) (allowed bool, rejectMethod string, err error) {
	if !g.enabled {
		return true, "", nil
	}
	method := strings.TrimPrefix(path, "/")
	if method == "" {
		if !g.registry.Allow(ctx, ip, g.plane, ratelimiter.MethodInvalid) {
			return false, ratelimiter.MethodInvalid, nil
		}
		return false, "", errInvalidURIMethod
	}
	if !g.registry.Allow(ctx, ip, g.plane, method) {
		return false, method, nil
	}
	return true, "", nil
}
