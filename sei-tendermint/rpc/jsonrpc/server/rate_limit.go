package server

import (
	"context"
	"errors"
	"io"
	"strings"

	"github.com/sei-protocol/sei-chain/ratelimiter"
)

// cometbftMethodCatalog is the fixed bucket label for browser catalog probes to /.
const cometbftMethodCatalog = "catalog"

var errInvalidURIMethod = errors.New("invalid URI method")

// RateLimitGate applies per-IP token-bucket rate limiting for CometBFT RPC HTTP
// requests. POST JSON-RPC bodies are parsed with MethodParser before full decode;
// GET URI routes are accounted by path-derived method names.
type RateLimitGate struct {
	*ratelimiter.Gate
}

// NewRateLimitGate returns a gate for CometBFT RPC HTTP (plane "cometbft").
// registry must be non-nil. maxBodyBytes is max-body-bytes from config; non-positive
// means unlimited and the gate does not apply its own body-size rejection (the outer
// MaxBytesHandler enforces a positive limit when configured).
func NewRateLimitGate(registry *ratelimiter.Registry, maxBodyBytes int64) *RateLimitGate {
	if maxBodyBytes <= 0 {
		maxBodyBytes = 0
	}
	return &RateLimitGate{
		Gate: ratelimiter.NewGate(registry, ratelimiter.PlaneCometBFT, maxBodyBytes),
	}
}

// CheckPOST parses body for JSON-RPC method names and applies per-IP rate limits.
func (g *RateLimitGate) CheckPOST(ctx context.Context, ip string, body io.Reader) (allowed bool, rejectMethod string, err error) {
	return g.CheckJSONRPC(ctx, ip, body)
}

// CheckCatalog applies per-IP rate limits for browser catalog probes to /.
func (g *RateLimitGate) CheckCatalog(ctx context.Context, ip string) (allowed bool, rejectMethod string) {
	if !g.Registry().Allow(ctx, ip, g.Plane(), cometbftMethodCatalog) {
		return false, cometbftMethodCatalog
	}
	return true, ""
}

// CheckURI applies per-IP rate limits for REST-style GET/HEAD RPC routes.
func (g *RateLimitGate) CheckURI(ctx context.Context, ip, path string) (allowed bool, rejectMethod string, err error) {
	method := strings.TrimPrefix(path, "/")
	if method == "" {
		if !g.Registry().Allow(ctx, ip, g.Plane(), ratelimiter.MethodInvalid) {
			return false, ratelimiter.MethodInvalid, nil
		}
		return false, "", errInvalidURIMethod
	}
	if !g.Registry().Allow(ctx, ip, g.Plane(), method) {
		return false, method, nil
	}
	return true, "", nil
}
