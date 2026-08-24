package ratelimiter

import (
	"context"
	"io"
	"math"
)

// Gate applies per-IP token-bucket rate limiting after extracting JSON-RPC
// method names from the request body (bounded by maxBodyBytes).
type Gate struct {
	registry     *Registry
	parser       *MethodParser
	maxBodyBytes int64
	plane        string
}

// NewGate returns a Gate for the given plane. registry must be non-nil.
// maxBodyBytes is passed through to MethodParser; callers normalize non-positive
// values according to their plane's policy before calling NewGate.
func NewGate(registry *Registry, plane string, maxBodyBytes int64) *Gate {
	if maxBodyBytes == math.MaxInt64 {
		maxBodyBytes = math.MaxInt64 - 1
	}
	return &Gate{
		registry:     registry,
		parser:       NewMethodParser(maxBodyBytes),
		maxBodyBytes: maxBodyBytes,
		plane:        plane,
	}
}

func (g *Gate) Registry() *Registry { return g.registry }
func (g *Gate) MaxBodyBytes() int64 { return g.maxBodyBytes }
func (g *Gate) Plane() string       { return g.plane }

// ChargeAdmissionRejection consumes one token for a fail-closed rejection that
// never reaches method parsing (oversize body, read error). Returns true when
// the bucket is exhausted and the caller should respond with HTTP 429.
func (g *Gate) ChargeAdmissionRejection(ctx context.Context, ip string) bool {
	return !g.registry.Allow(ctx, ip, g.plane, MethodInvalid)
}

// CheckJSONRPC parses body for JSON-RPC method names and applies per-IP rate limits.
// Parse errors still charge the bucket under MethodInvalid so malformed bodies
// can't bypass rate limiting.
func (g *Gate) CheckJSONRPC(ctx context.Context, ip string, body io.Reader) (allowed bool, rejectMethod string, err error) {
	methods, _, parseErr := g.parser.Parse(body)
	if parseErr != nil {
		if !g.registry.Allow(ctx, ip, g.plane, MethodInvalid) {
			return false, MethodInvalid, nil
		}
		return false, "", parseErr
	}

	if n := len(methods); n > 0 && !g.registry.AllowN(ctx, ip, g.plane, methods[0], n) {
		return false, methods[0], nil
	}
	return true, "", nil
}
