package ratelimiter

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/require"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/peer"

	"net"
)

// disabledCfg has rate limiting turned off.
var disabledCfg = Config{Enabled: false, RPS: 100, Burst: 10}

// zeroCfg has RPS=0 which also disables limiting.
var zeroCfg = Config{Enabled: true, RPS: 0, Burst: 10}

func cfg(rps float64, burst int, cidrs ...string) Config {
	return Config{Enabled: true, RPS: rps, Burst: burst, TrustedProxyCIDRs: cidrs}
}

// --- Allow ---

func TestAllow_DisabledAlwaysPasses(t *testing.T) {
	r := New(disabledCfg)
	for range 1000 {
		require.True(t, r.Allow(t.Context(), "1.2.3.4", "evm"))
	}
}

func TestAllow_ZeroRPSAlwaysPasses(t *testing.T) {
	r := New(zeroCfg)
	for range 1000 {
		require.True(t, r.Allow(t.Context(), "1.2.3.4", "evm"))
	}
}

func TestAllow_BurstThenReject(t *testing.T) {
	// burst=3, RPS tiny so no token refill during test
	r := New(cfg(0.001, 3))
	ip := "10.0.0.1"
	require.True(t, r.Allow(t.Context(), ip, "evm"), "first request in burst")
	require.True(t, r.Allow(t.Context(), ip, "evm"), "second request in burst")
	require.True(t, r.Allow(t.Context(), ip, "evm"), "third request in burst")
	require.False(t, r.Allow(t.Context(), ip, "evm"), "must be rejected after burst exhausted")
}

func TestAllow_PerIPIsolation(t *testing.T) {
	r := New(cfg(0.001, 1))
	require.True(t, r.Allow(t.Context(), "1.1.1.1", "evm"))
	require.False(t, r.Allow(t.Context(), "1.1.1.1", "evm"), "1.1.1.1 exhausted")
	// Different IP has its own independent bucket.
	require.True(t, r.Allow(t.Context(), "2.2.2.2", "evm"), "2.2.2.2 should still pass")
}

// --- IPFromHTTPRequest ---

func TestIPFromHTTPRequest_DirectConnection(t *testing.T) {
	r := New(cfg(100, 200)) // no trusted proxies
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req.RemoteAddr = "203.0.113.5:44321"
	req.Header.Set("X-Forwarded-For", "1.2.3.4")
	// RemoteAddr is not in trusted CIDRs, so XFF should be ignored.
	require.Equal(t, "203.0.113.5", r.IPFromHTTPRequest(req))
}

func TestIPFromHTTPRequest_TrustedProxy_XFF(t *testing.T) {
	r := New(cfg(100, 200, "10.0.0.0/8"))
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req.RemoteAddr = "10.0.0.1:12345"
	// Proxy appended 203.0.113.5 (real client) on the right; rightmost untrusted wins.
	req.Header.Set("X-Forwarded-For", "203.0.113.5, 10.0.0.1")
	require.Equal(t, "203.0.113.5", r.IPFromHTTPRequest(req))
}

func TestIPFromHTTPRequest_TrustedProxy_SpoofedXFF(t *testing.T) {
	r := New(cfg(100, 200, "10.0.0.0/8"))
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req.RemoteAddr = "10.0.0.1:12345"
	// Client pre-set a spoofed IP; proxy appended the real client IP on the right.
	// Must use rightmost untrusted (203.0.113.5), not the spoofed leftmost (1.2.3.4).
	req.Header.Set("X-Forwarded-For", "1.2.3.4, 203.0.113.5")
	require.Equal(t, "203.0.113.5", r.IPFromHTTPRequest(req))
}

func TestIPFromHTTPRequest_TrustedProxy_NoXFF(t *testing.T) {
	r := New(cfg(100, 200, "10.0.0.0/8"))
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req.RemoteAddr = "10.0.0.1:12345"
	// No XFF header: fall back to RemoteAddr.
	require.Equal(t, "10.0.0.1", r.IPFromHTTPRequest(req))
}

func TestIPFromHTTPRequest_UntrustedProxy_IgnoresXFF(t *testing.T) {
	// Only loopback is trusted.
	r := New(cfg(100, 200, "127.0.0.0/8"))
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req.RemoteAddr = "203.0.113.1:9999"
	req.Header.Set("X-Forwarded-For", "1.2.3.4")
	require.Equal(t, "203.0.113.1", r.IPFromHTTPRequest(req))
}

func TestIPFromHTTPRequest_SingleXFFEntry(t *testing.T) {
	r := New(cfg(100, 200, "127.0.0.0/8"))
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req.RemoteAddr = "127.0.0.1:8080"
	req.Header.Set("X-Forwarded-For", "  203.0.113.5  ")
	require.Equal(t, "203.0.113.5", r.IPFromHTTPRequest(req))
}

// --- IPFromGRPCContext ---

func grpcCtx(peerAddr string, xff ...string) context.Context {
	ctx := context.Background()
	ctx = peer.NewContext(ctx, &peer.Peer{
		Addr: mockAddr(peerAddr),
	})
	if len(xff) > 0 {
		md := metadata.Pairs("x-forwarded-for", xff[0])
		ctx = metadata.NewIncomingContext(ctx, md)
	}
	return ctx
}

type mockAddr string

func (a mockAddr) Network() string { return "tcp" }
func (a mockAddr) String() string  { return string(a) }

func TestIPFromGRPCContext_DirectPeer(t *testing.T) {
	r := New(cfg(100, 200)) // no trusted proxies
	ctx := grpcCtx("203.0.113.5:9000", "1.2.3.4")
	// Peer is not trusted, XFF must be ignored.
	require.Equal(t, "203.0.113.5", r.IPFromGRPCContext(ctx))
}

func TestIPFromGRPCContext_TrustedPeer_XFF(t *testing.T) {
	r := New(cfg(100, 200, "10.0.0.0/8"))
	// Proxy appended 203.0.113.5 (real client) on the right; rightmost untrusted wins.
	ctx := grpcCtx("10.0.0.2:9000", "203.0.113.5, 10.0.0.2")
	require.Equal(t, "203.0.113.5", r.IPFromGRPCContext(ctx))
}

func TestIPFromGRPCContext_TrustedPeer_SpoofedXFF(t *testing.T) {
	r := New(cfg(100, 200, "10.0.0.0/8"))
	// Client pre-set a spoofed IP; proxy appended the real client IP on the right.
	ctx := grpcCtx("10.0.0.2:9000", "1.2.3.4, 203.0.113.5")
	require.Equal(t, "203.0.113.5", r.IPFromGRPCContext(ctx))
}

func TestIPFromGRPCContext_TrustedPeer_NoMetadata(t *testing.T) {
	r := New(cfg(100, 200, "10.0.0.0/8"))
	ctx := grpcCtx("10.0.0.2:9000")
	require.Equal(t, "10.0.0.2", r.IPFromGRPCContext(ctx))
}

func TestIPFromGRPCContext_NoPeer(t *testing.T) {
	r := New(cfg(100, 200, "10.0.0.0/8"))
	require.Equal(t, "", r.IPFromGRPCContext(t.Context()))
}

// --- isTrustedProxy / parseCIDRs ---

func TestIsTrustedProxy_DefaultCIDRs(t *testing.T) {
	r := New(DefaultConfig)
	cases := []struct {
		ip      string
		trusted bool
	}{
		{"127.0.0.1", true},
		{"::1", true},
		{"10.1.2.3", true},
		{"172.16.0.1", true},
		{"192.168.1.1", true},
		{"203.0.113.1", false},
		{"8.8.8.8", false},
	}
	for _, tc := range cases {
		require.Equal(t, tc.trusted, r.isTrustedProxy(tc.ip), "ip=%s", tc.ip)
	}
}

func TestParseCIDRs_SkipsInvalid(t *testing.T) {
	nets := parseCIDRs([]string{"10.0.0.0/8", "not-a-cidr", "192.168.0.0/16"})
	require.Len(t, nets, 2)
}

func TestParseCIDRs_Empty(t *testing.T) {
	require.Empty(t, parseCIDRs(nil))
}

// --- helpers ---

func TestStripPort(t *testing.T) {
	require.Equal(t, "1.2.3.4", stripPort("1.2.3.4:8080"))
	require.Equal(t, "::1", stripPort("[::1]:9090"))
	require.Equal(t, "1.2.3.4", stripPort("1.2.3.4"))
}

func TestRightmostUntrustedIP(t *testing.T) {
	r := New(cfg(100, 200, "10.0.0.0/8", "127.0.0.0/8"))
	require.Equal(t, "203.0.113.5", r.rightmostUntrustedIP("203.0.113.5"))
	require.Equal(t, "203.0.113.5", r.rightmostUntrustedIP("1.2.3.4, 203.0.113.5"))
	require.Equal(t, "203.0.113.5", r.rightmostUntrustedIP("1.2.3.4, 203.0.113.5, 10.0.0.1"))
	require.Equal(t, "203.0.113.5", r.rightmostUntrustedIP("  203.0.113.5  "))        // whitespace stripped
	require.Equal(t, "", r.rightmostUntrustedIP("10.0.0.1, 127.0.0.1"))               // all trusted → empty
	require.Equal(t, "", r.rightmostUntrustedIP("not-an-ip"))                         // non-IP skipped
	require.Equal(t, "", r.rightmostUntrustedIP("not-an-ip, 10.0.0.1"))               // non-IP + trusted → empty, falls back to RemoteAddr
	require.Equal(t, "203.0.113.5", r.rightmostUntrustedIP("not-an-ip, 203.0.113.5")) // non-IP skipped, valid untrusted returned
}

// --- New validates TrustedProxyCIDRs ---

func TestNew_InvalidCIDRSkipped(t *testing.T) {
	r := New(Config{
		Enabled:           true,
		RPS:               100,
		Burst:             10,
		TrustedProxyCIDRs: []string{"bad", "10.0.0.0/8"},
	})
	require.Len(t, r.trustedProxies, 1)
	require.True(t, r.trustedProxies[0].Contains(net.ParseIP("10.1.1.1")))
}
