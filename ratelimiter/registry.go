package ratelimiter

import (
	"context"
	"net"
	"net/http"
	"strings"
	"time"

	"github.com/hashicorp/golang-lru/v2/expirable"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/metric"
	"golang.org/x/time/rate"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/peer"
)

const (
	DefaultRPS   = 200.0
	DefaultBurst = 400

	// lruSize bounds memory to ~8 MB at 50k entries (~160 bytes each).
	lruSize = 50_000
	// lruTTL evicts inactive IP entries after 1 hour.
	lruTTL = time.Hour
)

// DefaultTrustedProxyCIDRs contains RFC-1918 ranges and loopback addresses.
// Requests arriving from these CIDRs are trusted to supply a valid X-Forwarded-For header.
var DefaultTrustedProxyCIDRs = []string{
	"127.0.0.0/8",
	"::1/128",
	"10.0.0.0/8",
	"172.16.0.0/12",
	"192.168.0.0/16",
	"fc00::/7",
}

// Config holds the configuration for a Registry
type Config struct {
	// Enabled is a temporary rollout gate (Phase 1 only). False passes all requests through.
	Enabled bool
	// RPS is the sustained request rate allowed per IP in requests/second.
	// Zero disables per-IP rate limiting (all requests pass).
	RPS float64
	// Burst is the maximum number of requests allowed in a single burst.
	Burst int
	// TrustedProxyCIDRs lists CIDRs whose X-Forwarded-For headers are trusted.
	// Empty means trust no proxy; use RemoteAddr / peer address directly.
	TrustedProxyCIDRs []string
}

var DefaultConfig = Config{
	Enabled:           true,
	RPS:               DefaultRPS,
	Burst:             DefaultBurst,
	TrustedProxyCIDRs: DefaultTrustedProxyCIDRs,
}

// Registry is a per-IP token-bucket rate limiter backed by an expirable LRU.
// It is safe for concurrent use.
type Registry struct {
	cfg            Config
	trustedProxies []*net.IPNet
	lru            *expirable.LRU[string, *rate.Limiter]
}

// New creates a Registry from cfg. Invalid CIDRs in TrustedProxyCIDRs are silently skipped.
func New(cfg Config) *Registry {
	return &Registry{
		cfg:            cfg,
		trustedProxies: parseCIDRs(cfg.TrustedProxyCIDRs),
		lru:            expirable.NewLRU[string, *rate.Limiter](lruSize, nil, lruTTL),
	}
}

// Allow reports whether the request from ip should be allowed for the given plane.
// Rejections increment rpc_rate_limit_rejected_total{plane}.
func (r *Registry) Allow(ctx context.Context, ip, plane string) bool {
	if !r.cfg.Enabled || r.cfg.RPS == 0 {
		return true
	}
	if r.getOrCreate(ip).Allow() {
		return true
	}
	registryMetrics.rejectedCounter.Add(
		ctx,
		1,
		metric.WithAttributes(
			attribute.String("plane", plane),
		),
	)
	return false
}

// IPFromHTTPRequest extracts the client IP from an HTTP request.
// If RemoteAddr belongs to a trusted proxy CIDR, the rightmost untrusted X-Forwarded-For
// entry is used. Walking right-to-left and skipping trusted CIDRs prevents a client from
// spoofing their IP by pre-setting X-Forwarded-For before the request reaches the proxy.
func (r *Registry) IPFromHTTPRequest(req *http.Request) string {
	remoteIP := stripPort(req.RemoteAddr)
	if r.isTrustedProxy(remoteIP) {
		if xff := strings.Join(req.Header.Values("X-Forwarded-For"), ", "); xff != "" {
			if ip := r.rightmostUntrustedIP(xff); ip != "" {
				return ip
			}
		}
	}
	return remoteIP
}

// IPFromGRPCContext extracts the client IP from a gRPC request context.
// If the transport peer belongs to a trusted proxy CIDR, the rightmost untrusted
// x-forwarded-for metadata entry is used.
func (r *Registry) IPFromGRPCContext(ctx context.Context) string {
	peerIP := grpcPeerIP(ctx)
	if peerIP != "" && r.isTrustedProxy(peerIP) {
		if md, ok := metadata.FromIncomingContext(ctx); ok {
			if vals := md.Get("x-forwarded-for"); len(vals) > 0 {
				if ip := r.rightmostUntrustedIP(strings.Join(vals, ", ")); ip != "" {
					return ip
				}
			}
		}
	}
	return peerIP
}

// rightmostUntrustedIP walks the comma-separated XFF list from right to left and returns
// the first IP that is not in TrustedProxyCIDRs. This is the real client IP: proxies
// append their view of the source address, so the rightmost untrusted entry cannot be
// forged by the client.
func (r *Registry) rightmostUntrustedIP(xff string) string {
	parts := strings.Split(xff, ",")
	for i := len(parts) - 1; i >= 0; i-- {
		candidate := strings.TrimSpace(parts[i])
		if net.ParseIP(candidate) == nil {
			continue
		}
		if !r.isTrustedProxy(candidate) {
			return candidate
		}
	}
	return ""
}

// getOrCreate returns the existing limiter for ip or creates a fresh one.
// A brief TOCTOU race can occur under high concurrency: at most one extra burst token
// leaks per race window, which has no meaningful security impact.
func (r *Registry) getOrCreate(ip string) *rate.Limiter {
	if l, ok := r.lru.Get(ip); ok {
		return l
	}
	l := rate.NewLimiter(rate.Limit(r.cfg.RPS), r.cfg.Burst)
	r.lru.Add(ip, l)
	return l
}

func (r *Registry) isTrustedProxy(ip string) bool {
	parsed := net.ParseIP(ip)
	if parsed == nil {
		return false
	}
	for _, n := range r.trustedProxies {
		if n.Contains(parsed) {
			return true
		}
	}
	return false
}

func parseCIDRs(cidrs []string) []*net.IPNet {
	out := make([]*net.IPNet, 0, len(cidrs))
	for _, cidr := range cidrs {
		if _, network, err := net.ParseCIDR(cidr); err == nil {
			out = append(out, network)
		}
	}
	return out
}

func stripPort(addr string) string {
	host, _, err := net.SplitHostPort(addr)
	if err != nil {
		return addr
	}
	return host
}

func grpcPeerIP(ctx context.Context) string {
	p, ok := peer.FromContext(ctx)
	if !ok || p.Addr == nil {
		return ""
	}
	return stripPort(p.Addr.String())
}
