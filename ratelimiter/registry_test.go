package ratelimiter

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/require"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	sdkmetric "go.opentelemetry.io/otel/sdk/metric"
	"go.opentelemetry.io/otel/sdk/metric/metricdata"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/peer"
)

// zeroCfg has RPS=0 which disables limiting.
var zeroCfg = Config{RPS: 0, Burst: 10}

// negCfg has RPS<0 which disables limiting.
var negCfg = Config{RPS: -1, Burst: 10}

// zeroBurstCfg has Burst=0 which disables limiting.
var zeroBurstCfg = Config{RPS: 100, Burst: 0}

func cfg(rps float64, burst int, cidrs ...string) Config {
	return Config{RPS: rps, Burst: burst, TrustedProxyCIDRs: cidrs}
}

func mustNew(t *testing.T, c Config) *Registry {
	t.Helper()
	r, err := New(c)
	require.NoError(t, err)
	return r
}

// --- Allow ---

func TestAllow_ZeroRPSAlwaysPasses(t *testing.T) {
	r := mustNew(t, zeroCfg)
	for range 1000 {
		require.True(t, r.Allow(t.Context(), "1.2.3.4", "evm", "eth_call"))
	}
}

func TestAllow_NegativeRPSAlwaysPasses(t *testing.T) {
	r := mustNew(t, negCfg)
	for range 1000 {
		require.True(t, r.Allow(t.Context(), "1.2.3.4", "evm", "eth_call"))
	}
}

func TestAllow_ZeroBurstAlwaysPasses(t *testing.T) {
	r := mustNew(t, zeroBurstCfg)
	for range 1000 {
		require.True(t, r.Allow(t.Context(), "1.2.3.4", "evm", "eth_call"))
	}
}

func TestAllow_BurstThenReject(t *testing.T) {
	// burst=3, RPS tiny so no token refill during test
	r := mustNew(t, cfg(0.001, 3))
	ip := "10.0.0.1"
	require.True(t, r.Allow(t.Context(), ip, "evm", "eth_call"), "first request in burst")
	require.True(t, r.Allow(t.Context(), ip, "evm", "eth_call"), "second request in burst")
	require.True(t, r.Allow(t.Context(), ip, "evm", "eth_call"), "third request in burst")
	require.False(t, r.Allow(t.Context(), ip, "evm", "eth_call"), "must be rejected after burst exhausted")
}

func TestAllow_PerIPIsolation(t *testing.T) {
	r := mustNew(t, cfg(0.001, 1))
	require.True(t, r.Allow(t.Context(), "1.1.1.1", "evm", "eth_call"))
	require.False(t, r.Allow(t.Context(), "1.1.1.1", "evm", "eth_call"), "1.1.1.1 exhausted")
	// Different IP has its own independent bucket.
	require.True(t, r.Allow(t.Context(), "2.2.2.2", "evm", "eth_call"), "2.2.2.2 should still pass")
}

func TestAllow_RejectionRecordsMethodMetric(t *testing.T) {
	reader := sdkmetric.NewManualReader()
	provider := sdkmetric.NewMeterProvider(sdkmetric.WithReader(reader))
	prev := otel.GetMeterProvider()
	otel.SetMeterProvider(provider)
	t.Cleanup(func() { otel.SetMeterProvider(prev) })

	// Re-init metrics against the test provider.
	registryMetrics.rejectedCounter = must(provider.Meter("ratelimiter").Int64Counter(
		"rpc_rate_limit_rejected_total",
	))

	r := mustNew(t, cfg(0.001, 1))
	ip := "10.0.0.42"
	require.True(t, r.Allow(t.Context(), ip, "evm", "eth_call"))
	require.False(t, r.Allow(t.Context(), ip, "evm", "eth_getBalance"))

	var rm metricdata.ResourceMetrics
	require.NoError(t, reader.Collect(t.Context(), &rm))
	require.NotEmpty(t, rm.ScopeMetrics)
	found := false
	for _, sm := range rm.ScopeMetrics {
		for _, m := range sm.Metrics {
			if m.Name != "rpc_rate_limit_rejected_total" {
				continue
			}
			sum := m.Data.(metricdata.Sum[int64])
			require.Equal(t, int64(1), sum.DataPoints[0].Value)
			attrs := sum.DataPoints[0].Attributes.ToSlice()
			require.Contains(t, attrs, attribute.String("plane", "evm"))
			require.Contains(t, attrs, attribute.String("method", "eth_getBalance"))
			found = true
		}
	}
	require.True(t, found, "expected rpc_rate_limit_rejected_total metric")
}

func TestAllow_IPv6_SamePrefixSharesBucket(t *testing.T) {
	r := mustNew(t, cfg(0.001, 1))
	require.True(t, r.Allow(t.Context(), "2001:db8::1", "evm", "eth_call"), "first address in /64 passes")
	require.False(t, r.Allow(t.Context(), "2001:db8::2", "evm", "eth_call"), "different address in same /64 rejected")
}

func TestAllow_IPv6_DifferentPrefixOwnBucket(t *testing.T) {
	r := mustNew(t, cfg(0.001, 1))
	require.True(t, r.Allow(t.Context(), "2001:db8:0:1::1", "evm", "eth_call"))
	require.True(t, r.Allow(t.Context(), "2001:db8:0:2::1", "evm", "eth_call"), "different /64 has its own bucket")
}

// --- IPFromHTTPRequest ---

func TestIPFromHTTPRequest_DirectConnection(t *testing.T) {
	r := mustNew(t, cfg(100, 200)) // no trusted proxies
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req.RemoteAddr = "203.0.113.5:44321"
	req.Header.Set("X-Forwarded-For", "1.2.3.4")
	// RemoteAddr is not in trusted CIDRs, so XFF should be ignored.
	require.Equal(t, "203.0.113.5", r.IPFromHTTPRequest(req))
}

func TestIPFromHTTPRequest_TrustedProxy_XFF(t *testing.T) {
	r := mustNew(t, cfg(100, 200, "10.0.0.0/8"))
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req.RemoteAddr = "10.0.0.1:12345"
	// Proxy appended 203.0.113.5 (real client) on the right; rightmost untrusted wins.
	req.Header.Set("X-Forwarded-For", "203.0.113.5, 10.0.0.1")
	require.Equal(t, "203.0.113.5", r.IPFromHTTPRequest(req))
}

func TestIPFromHTTPRequest_TrustedProxy_SpoofedXFF(t *testing.T) {
	r := mustNew(t, cfg(100, 200, "10.0.0.0/8"))
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req.RemoteAddr = "10.0.0.1:12345"
	// Client pre-set a spoofed IP; proxy appended the real client IP on the right.
	// Must use rightmost untrusted (203.0.113.5), not the spoofed leftmost (1.2.3.4).
	req.Header.Set("X-Forwarded-For", "1.2.3.4, 203.0.113.5")
	require.Equal(t, "203.0.113.5", r.IPFromHTTPRequest(req))
}

func TestIPFromHTTPRequest_TrustedProxy_NoXFF(t *testing.T) {
	r := mustNew(t, cfg(100, 200, "10.0.0.0/8"))
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req.RemoteAddr = "10.0.0.1:12345"
	// No XFF header: fall back to RemoteAddr.
	require.Equal(t, "10.0.0.1", r.IPFromHTTPRequest(req))
}

func TestIPFromHTTPRequest_UntrustedProxy_IgnoresXFF(t *testing.T) {
	// Only loopback is trusted.
	r := mustNew(t, cfg(100, 200, "127.0.0.0/8"))
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req.RemoteAddr = "203.0.113.1:9999"
	req.Header.Set("X-Forwarded-For", "1.2.3.4")
	require.Equal(t, "203.0.113.1", r.IPFromHTTPRequest(req))
}

func TestIPFromHTTPRequest_SingleXFFEntry(t *testing.T) {
	r := mustNew(t, cfg(100, 200, "127.0.0.0/8"))
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req.RemoteAddr = "127.0.0.1:8080"
	req.Header.Set("X-Forwarded-For", "  203.0.113.5  ")
	require.Equal(t, "203.0.113.5", r.IPFromHTTPRequest(req))
}

func TestIPFromHTTPRequest_MultipleXFFHeaders_SpoofPrevented(t *testing.T) {
	r := mustNew(t, cfg(100, 200, "10.0.0.0/8"))
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req.RemoteAddr = "10.0.0.1:12345"
	// Client pre-sets a spoofed IP as the first XFF header line.
	// Proxy adds the real client IP as a separate header line (not appended to the existing one).
	req.Header.Add("X-Forwarded-For", "spoofed-ip-ignored")
	req.Header.Add("X-Forwarded-For", "203.0.113.5")
	// Must use rightmost untrusted across all header lines, not just the first.
	require.Equal(t, "203.0.113.5", r.IPFromHTTPRequest(req))
}

// --- IPFromGRPCContext ---

func grpcCtx(peerAddr string, xff ...string) context.Context {
	ctx := context.Background()
	ctx = peer.NewContext(ctx, &peer.Peer{
		Addr: mockAddr(peerAddr),
	})
	if len(xff) > 0 {
		md := metadata.MD{"x-forwarded-for": xff}
		ctx = metadata.NewIncomingContext(ctx, md)
	}
	return ctx
}

type mockAddr string

func (a mockAddr) Network() string { return "tcp" }
func (a mockAddr) String() string  { return string(a) }

func TestIPFromGRPCContext_DirectPeer(t *testing.T) {
	r := mustNew(t, cfg(100, 200)) // no trusted proxies
	ctx := grpcCtx("203.0.113.5:9000", "1.2.3.4")
	// Peer is not trusted, XFF must be ignored.
	require.Equal(t, "203.0.113.5", r.IPFromGRPCContext(ctx))
}

func TestIPFromGRPCContext_TrustedPeer_XFF(t *testing.T) {
	r := mustNew(t, cfg(100, 200, "10.0.0.0/8"))
	// Proxy appended 203.0.113.5 (real client) on the right; rightmost untrusted wins.
	ctx := grpcCtx("10.0.0.2:9000", "203.0.113.5, 10.0.0.2")
	require.Equal(t, "203.0.113.5", r.IPFromGRPCContext(ctx))
}

func TestIPFromGRPCContext_TrustedPeer_SpoofedXFF(t *testing.T) {
	r := mustNew(t, cfg(100, 200, "10.0.0.0/8"))
	// Client pre-set a spoofed IP; proxy appended the real client IP on the right.
	ctx := grpcCtx("10.0.0.2:9000", "1.2.3.4, 203.0.113.5")
	require.Equal(t, "203.0.113.5", r.IPFromGRPCContext(ctx))
}

func TestIPFromGRPCContext_MultipleXFFValues_SpoofPrevented(t *testing.T) {
	r := mustNew(t, cfg(100, 200, "10.0.0.0/8"))
	// Client pre-sets a spoofed IP as the first metadata value; proxy appends real IP as a second value.
	ctx := grpcCtx("10.0.0.2:9000", "spoofed-ip-ignored", "203.0.113.5")
	require.Equal(t, "203.0.113.5", r.IPFromGRPCContext(ctx))
}

func TestIPFromGRPCContext_TrustedPeer_NoMetadata(t *testing.T) {
	r := mustNew(t, cfg(100, 200, "10.0.0.0/8"))
	ctx := grpcCtx("10.0.0.2:9000")
	require.Equal(t, "10.0.0.2", r.IPFromGRPCContext(ctx))
}

func TestIPFromGRPCContext_NoPeer(t *testing.T) {
	r := mustNew(t, cfg(100, 200, "10.0.0.0/8"))
	require.Equal(t, "", r.IPFromGRPCContext(t.Context()))
}

// --- isTrustedProxy / parseCIDRs ---

func TestIsTrustedProxy_DefaultConfig_NoProxies(t *testing.T) {
	r := mustNew(t, DefaultConfig)
	// DefaultConfig has no trusted proxies; every IP is untrusted.
	for _, ip := range []string{"127.0.0.1", "::1", "10.1.2.3", "172.16.0.1", "192.168.1.1"} {
		require.False(t, r.isTrustedProxy(ip), "ip=%s", ip)
	}
}

func TestIsTrustedProxy_DefaultTrustedProxyCIDRs(t *testing.T) {
	r := mustNew(t, Config{RPS: 100, Burst: 10, TrustedProxyCIDRs: DefaultTrustedProxyCIDRs})
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

func TestParseCIDRs_ReturnsErrorOnInvalid(t *testing.T) {
	_, err := parseCIDRs([]string{"10.0.0.0/8", "not-a-cidr", "192.168.0.0/16"})
	require.ErrorContains(t, err, "not-a-cidr")
}

func TestParseCIDRs_Empty(t *testing.T) {
	nets, err := parseCIDRs(nil)
	require.NoError(t, err)
	require.Empty(t, nets)
}

// --- helpers ---

func TestBucketKey(t *testing.T) {
	// IPv4: canonical dotted form
	require.Equal(t, "1.2.3.4", bucketKey("1.2.3.4"))
	// IPv4-mapped IPv6: canonicalized to dotted form so both forms share one bucket
	require.Equal(t, "1.2.3.4", bucketKey("::ffff:1.2.3.4"))
	// IPv6: masked to /64
	require.Equal(t, "2001:db8::", bucketKey("2001:db8::1"))
	require.Equal(t, "2001:db8::", bucketKey("2001:db8::ffff"))
	require.Equal(t, "2001:db8:1:2::", bucketKey("2001:db8:1:2:3:4:5:6"))
	// Unparseable: passed through unchanged
	require.Equal(t, "not-an-ip", bucketKey("not-an-ip"))
}

func TestStripPort(t *testing.T) {
	require.Equal(t, "1.2.3.4", stripPort("1.2.3.4:8080"))
	require.Equal(t, "::1", stripPort("[::1]:9090"))
	require.Equal(t, "1.2.3.4", stripPort("1.2.3.4"))
}

func TestRightmostUntrustedIP(t *testing.T) {
	r := mustNew(t, cfg(100, 200, "10.0.0.0/8", "127.0.0.0/8"))
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

func TestNew_InvalidCIDR_ReturnsError(t *testing.T) {
	_, err := New(Config{
		RPS:               100,
		Burst:             10,
		TrustedProxyCIDRs: []string{"bad", "10.0.0.0/8"},
	})
	require.ErrorContains(t, err, "bad")
}
