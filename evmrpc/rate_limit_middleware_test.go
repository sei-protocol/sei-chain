package evmrpc

import (
	"io"
	"math"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/sei-protocol/sei-chain/ratelimiter"
	"github.com/stretchr/testify/require"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	sdkmetric "go.opentelemetry.io/otel/sdk/metric"
	"go.opentelemetry.io/otel/sdk/metric/metricdata"
)

func mustRateLimitRegistry(t *testing.T, rps float64, burst int) *ratelimiter.Registry {
	t.Helper()
	reg, err := ratelimiter.New(ratelimiter.Config{RPS: rps, Burst: burst})
	require.NoError(t, err)
	return reg
}

func TestRateLimitMiddleware_AllowsUnderLimit(t *testing.T) {
	reg := mustRateLimitRegistry(t, 100, 10)
	gate := NewRateLimitGate(reg, 0, true, "evm")

	called := false
	inner := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		called = true
		body, err := io.ReadAll(r.Body)
		require.NoError(t, err)
		require.JSONEq(t, `{"jsonrpc":"2.0","id":1,"method":"eth_call","params":[]}`, string(body))
		w.WriteHeader(http.StatusOK)
	})

	h := newRateLimitMiddleware(inner, gate)
	req := httptest.NewRequest(http.MethodPost, "/", strings.NewReader(
		`{"jsonrpc":"2.0","id":1,"method":"eth_call","params":[]}`,
	))
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)

	require.Equal(t, http.StatusOK, rec.Code)
	require.True(t, called)
}

func TestRateLimitMiddleware_RejectsAfterBurst(t *testing.T) {
	reg := mustRateLimitRegistry(t, 0.001, 1)
	gate := NewRateLimitGate(reg, 0, true, "evm")
	inner := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	})
	h := newRateLimitMiddleware(inner, gate)

	body := `{"jsonrpc":"2.0","id":1,"method":"eth_call","params":[]}`
	req1 := httptest.NewRequest(http.MethodPost, "/", strings.NewReader(body))
	req1.RemoteAddr = "203.0.113.1:1234"
	rec1 := httptest.NewRecorder()
	h.ServeHTTP(rec1, req1)
	require.Equal(t, http.StatusOK, rec1.Code)

	req2 := httptest.NewRequest(http.MethodPost, "/", strings.NewReader(body))
	req2.RemoteAddr = "203.0.113.1:1234"
	rec2 := httptest.NewRecorder()
	h.ServeHTTP(rec2, req2)
	require.Equal(t, http.StatusTooManyRequests, rec2.Code)
	require.Contains(t, rec2.Body.String(), "too many requests")
}

func TestRateLimitMiddleware_PerIPIsolation(t *testing.T) {
	reg := mustRateLimitRegistry(t, 0.001, 1)
	gate := NewRateLimitGate(reg, 0, true, "evm")
	inner := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusOK) })
	h := newRateLimitMiddleware(inner, gate)
	body := `{"jsonrpc":"2.0","id":1,"method":"eth_call","params":[]}`

	reqA := httptest.NewRequest(http.MethodPost, "/", strings.NewReader(body))
	reqA.RemoteAddr = "203.0.113.1:1"
	recA := httptest.NewRecorder()
	h.ServeHTTP(recA, reqA)
	require.Equal(t, http.StatusOK, recA.Code)

	reqA2 := httptest.NewRequest(http.MethodPost, "/", strings.NewReader(body))
	reqA2.RemoteAddr = "203.0.113.1:1"
	recA2 := httptest.NewRecorder()
	h.ServeHTTP(recA2, reqA2)
	require.Equal(t, http.StatusTooManyRequests, recA2.Code)

	reqB := httptest.NewRequest(http.MethodPost, "/", strings.NewReader(body))
	reqB.RemoteAddr = "203.0.113.2:1"
	recB := httptest.NewRecorder()
	h.ServeHTTP(recB, reqB)
	require.Equal(t, http.StatusOK, recB.Code)
}

func TestRateLimitMiddleware_BatchCountsAllMethods(t *testing.T) {
	reg := mustRateLimitRegistry(t, 0.001, 2)
	gate := NewRateLimitGate(reg, 0, true, "evm")
	inner := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusOK) })
	h := newRateLimitMiddleware(inner, gate)

	// First batch consumes 2 tokens (burst=2).
	batch := `[{"method":"eth_call","id":1},{"method":"eth_getBalance","id":2}]`
	req1 := httptest.NewRequest(http.MethodPost, "/", strings.NewReader(batch))
	req1.RemoteAddr = "10.0.0.5:1"
	rec1 := httptest.NewRecorder()
	h.ServeHTTP(rec1, req1)
	require.Equal(t, http.StatusOK, rec1.Code)

	// Second batch needs 1 token but bucket is empty.
	req2 := httptest.NewRequest(http.MethodPost, "/", strings.NewReader(
		`{"jsonrpc":"2.0","id":1,"method":"eth_call","params":[]}`,
	))
	req2.RemoteAddr = "10.0.0.5:1"
	rec2 := httptest.NewRecorder()
	h.ServeHTTP(rec2, req2)
	require.Equal(t, http.StatusTooManyRequests, rec2.Code)
}

func TestRateLimitMiddleware_ProbeLimitRejected413(t *testing.T) {
	reg := mustRateLimitRegistry(t, 100, 10)
	gate := NewRateLimitGate(reg, 64, true, "evm")

	padding := strings.Repeat(" ", 50)
	body := `{"params":[` + padding + `],"method":"eth_call","id":1}`

	inner := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		t.Fatal("inner should not be called")
	})
	h := newRateLimitMiddleware(inner, gate)

	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodPost, "/", strings.NewReader(body)))
	require.Equal(t, http.StatusRequestEntityTooLarge, rec.Code)
	require.Contains(t, rec.Body.String(), "request body too large")
}

func TestRateLimitMiddleware_OversizeChargesPerIP(t *testing.T) {
	reg := mustRateLimitRegistry(t, 0.001, 1)
	gate := NewRateLimitGate(reg, 64, true, "evm")
	inner := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		t.Fatal("inner should not be called")
	})
	h := newRateLimitMiddleware(inner, gate)

	remote := "203.0.113.88:1"
	oversizeBody := strings.Repeat("x", 100)

	req1 := httptest.NewRequest(http.MethodPost, "/", strings.NewReader(oversizeBody))
	req1.RemoteAddr = remote
	rec1 := httptest.NewRecorder()
	h.ServeHTTP(rec1, req1)
	require.Equal(t, http.StatusRequestEntityTooLarge, rec1.Code)

	req2 := httptest.NewRequest(http.MethodPost, "/", strings.NewReader(oversizeBody))
	req2.RemoteAddr = remote
	rec2 := httptest.NewRecorder()
	h.ServeHTTP(rec2, req2)
	require.Equal(t, http.StatusTooManyRequests, rec2.Code)
	require.Contains(t, rec2.Body.String(), "too many requests")
}

func TestComposedStack_ChunkedOversizeRateLimitedAfterBurst(t *testing.T) {
	const maxBody = 100
	reg := mustRateLimitRegistry(t, 0.001, 1)
	gate := NewRateLimitGate(reg, maxBody, true, "evm")

	inner := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		t.Fatal("inner should not be called")
	})
	stack := newRequestSizeLimiter(newRateLimitMiddleware(inner, gate), maxBody, 0, 0)

	remote := "203.0.113.89:1"
	oversizeBody := strings.Repeat("x", maxBody+64)

	req1 := httptest.NewRequest(http.MethodPost, "/", strings.NewReader(oversizeBody))
	req1.ContentLength = -1
	req1.RemoteAddr = remote
	rec1 := httptest.NewRecorder()
	stack.ServeHTTP(rec1, req1)
	require.Equal(t, http.StatusRequestEntityTooLarge, rec1.Code)

	req2 := httptest.NewRequest(http.MethodPost, "/", strings.NewReader(oversizeBody))
	req2.ContentLength = -1
	req2.RemoteAddr = remote
	rec2 := httptest.NewRecorder()
	stack.ServeHTTP(rec2, req2)
	require.Equal(t, http.StatusTooManyRequests, rec2.Code)
}

func TestRateLimitMiddleware_ParseErrorRejected(t *testing.T) {
	reg := mustRateLimitRegistry(t, 100, 10)
	gate := NewRateLimitGate(reg, 0, true, "evm")
	inner := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		t.Fatal("inner should not be called")
	})
	h := newRateLimitMiddleware(inner, gate)

	req := httptest.NewRequest(http.MethodPost, "/", strings.NewReader(`{"method":123}`))
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	require.Equal(t, http.StatusBadRequest, rec.Code)
}

func TestRateLimitMiddleware_DisabledBypasses(t *testing.T) {
	reg := mustRateLimitRegistry(t, 0.001, 1)
	gate := NewRateLimitGate(reg, 0, false, "evm")
	inner := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusOK) })
	h := newRateLimitMiddleware(inner, gate)
	body := `{"jsonrpc":"2.0","id":1,"method":"eth_call","params":[]}`

	for range 3 {
		req := httptest.NewRequest(http.MethodPost, "/", strings.NewReader(body))
		req.RemoteAddr = "10.0.0.1:1"
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, req)
		require.Equal(t, http.StatusOK, rec.Code)
	}
}

func TestRateLimitMiddleware_HealthCheckPassthrough(t *testing.T) {
	reg := mustRateLimitRegistry(t, 0.001, 1)
	gate := NewRateLimitGate(reg, 0, true, "evm")
	called := false
	inner := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		called = true
		w.WriteHeader(http.StatusOK)
	})
	h := newRateLimitMiddleware(inner, gate)

	for _, method := range []string{http.MethodGet, http.MethodHead} {
		t.Run(method, func(t *testing.T) {
			called = false
			req := httptest.NewRequest(method, "/", nil)
			rec := httptest.NewRecorder()
			h.ServeHTTP(rec, req)
			require.Equal(t, http.StatusOK, rec.Code)
			require.True(t, called)
		})
	}
}

func TestRateLimitMiddleware_OptionsPassthrough(t *testing.T) {
	reg := mustRateLimitRegistry(t, 0.001, 1)
	gate := NewRateLimitGate(reg, 0, true, "evm")
	called := false
	inner := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		called = true
		w.WriteHeader(http.StatusOK)
	})
	h := newRateLimitMiddleware(inner, gate)

	req := httptest.NewRequest(http.MethodOptions, "/", nil)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	require.Equal(t, http.StatusOK, rec.Code)
	require.True(t, called)
}

func TestRateLimitMiddleware_GetWithBodyRateLimitedLikePost(t *testing.T) {
	reg := mustRateLimitRegistry(t, 0.001, 1)
	gate := NewRateLimitGate(reg, 0, true, "evm")
	inner := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	})
	h := newRateLimitMiddleware(inner, gate)

	body := `{"jsonrpc":"2.0","id":1,"method":"eth_call","params":[]}`
	remote := "203.0.113.99:1"

	req1 := httptest.NewRequest(http.MethodGet, "/", strings.NewReader(body))
	req1.RemoteAddr = remote
	req1.Header.Set("Content-Type", "application/json")
	rec1 := httptest.NewRecorder()
	h.ServeHTTP(rec1, req1)
	require.Equal(t, http.StatusOK, rec1.Code)

	req2 := httptest.NewRequest(http.MethodGet, "/", strings.NewReader(body))
	req2.RemoteAddr = remote
	req2.Header.Set("Content-Type", "application/json")
	rec2 := httptest.NewRecorder()
	h.ServeHTTP(rec2, req2)
	require.Equal(t, http.StatusTooManyRequests, rec2.Code)
	require.Contains(t, rec2.Body.String(), "too many requests")
}

func TestComposedStack_RateLimitDistinctFromSizeBudget(t *testing.T) {
	const maxBody = 4096
	reg := mustRateLimitRegistry(t, 0.001, 1)
	gate := NewRateLimitGate(reg, 0, true, "evm")
	enabled := BuildSeiLegacyEnabledSet([]string{"sei_getTransactionReceipt"})

	body := `{"jsonrpc":"2.0","id":1,"method":"eth_call","params":[]}`
	base := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = io.ReadAll(r.Body)
		w.WriteHeader(http.StatusOK)
	})
	stack := newRequestSizeLimiter(
		newRateLimitMiddleware(wrapSeiLegacyHTTP(base, enabled, maxBody), gate),
		maxBody,
		0,
		0,
	)

	req1 := httptest.NewRequest(http.MethodPost, "/", strings.NewReader(body))
	req1.RemoteAddr = "198.51.100.7:1"
	rec1 := httptest.NewRecorder()
	stack.ServeHTTP(rec1, req1)
	require.Equal(t, http.StatusOK, rec1.Code)

	req2 := httptest.NewRequest(http.MethodPost, "/", strings.NewReader(body))
	req2.RemoteAddr = "198.51.100.7:1"
	rec2 := httptest.NewRecorder()
	stack.ServeHTTP(rec2, req2)
	require.Equal(t, http.StatusTooManyRequests, rec2.Code)
	require.Contains(t, rec2.Body.String(), "too many requests")
}

func TestNewRateLimitGate_MaxInt64BodyLimitClamped(t *testing.T) {
	reg := mustRateLimitRegistry(t, 100, 10)
	gate := NewRateLimitGate(reg, math.MaxInt64, true, "evm")
	require.Equal(t, int64(math.MaxInt64-1), gate.maxBodyBytes)

	body := `{"jsonrpc":"2.0","id":1,"method":"eth_call","params":[]}`
	inner := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusOK) })
	h := newRateLimitMiddleware(inner, gate)

	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodPost, "/", strings.NewReader(body)))
	require.Equal(t, http.StatusOK, rec.Code)
}

func TestComposedStack_OversizeContentLengthBeforeProbeRead(t *testing.T) {
	const maxBody = 100
	reg := mustRateLimitRegistry(t, 100, 10)
	gate := NewRateLimitGate(reg, 0, true, "evm")

	var bodyRead bool
	base := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		b, _ := io.ReadAll(r.Body)
		bodyRead = len(b) > 0
	})
	stack := newRequestSizeLimiter(newRateLimitMiddleware(base, gate), maxBody, 0, 0)

	tracked := &trackedBody{Reader: strings.NewReader(strings.Repeat("x", 200))}
	req := httptest.NewRequest(http.MethodPost, "/", tracked)
	req.ContentLength = 200

	rec := httptest.NewRecorder()
	stack.ServeHTTP(rec, req)

	require.Equal(t, http.StatusRequestEntityTooLarge, rec.Code)
	require.False(t, bodyRead)
	require.Equal(t, int64(0), tracked.drained, "oversize body must be rejected before probe read")
}

func TestComposedStack_ChunkedOversizeReturns413(t *testing.T) {
	const maxBody = 100
	reg := mustRateLimitRegistry(t, 100, 10)
	gate := NewRateLimitGate(reg, maxBody, true, "evm")

	var innerRan bool
	base := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		innerRan = true
		w.WriteHeader(http.StatusOK)
	})
	stack := newRequestSizeLimiter(newRateLimitMiddleware(base, gate), maxBody, 0, 0)

	req := httptest.NewRequest(http.MethodPost, "/", strings.NewReader(strings.Repeat("x", maxBody+64)))
	req.ContentLength = -1

	rec := httptest.NewRecorder()
	stack.ServeHTTP(rec, req)

	require.Equal(t, http.StatusRequestEntityTooLarge, rec.Code)
	require.Contains(t, rec.Body.String(), "request body too large")
	require.False(t, innerRan)
}

func TestRateLimitMiddleware_MaxBytesReaderOversizeReturns413(t *testing.T) {
	const maxBody = 100
	reg := mustRateLimitRegistry(t, 100, 10)
	gate := NewRateLimitGate(reg, maxBody, true, "evm")
	inner := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		t.Fatal("inner should not be called")
	})
	h := newRateLimitMiddleware(inner, gate)

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/", strings.NewReader(strings.Repeat("x", maxBody+64)))
	req.Body = http.MaxBytesReader(rec, req.Body, maxBody)
	req.ContentLength = -1

	h.ServeHTTP(rec, req)

	require.Equal(t, http.StatusRequestEntityTooLarge, rec.Code)
	require.Contains(t, rec.Body.String(), "request body too large")
}

func TestRateLimitGate_Check(t *testing.T) {
	reg := mustRateLimitRegistry(t, 0.001, 1)
	gate := NewRateLimitGate(reg, 0, true, "evm")

	allowed, _, err := gate.Check(t.Context(), "1.2.3.4", strings.NewReader(
		`{"method":"eth_call","id":1}`,
	))
	require.NoError(t, err)
	require.True(t, allowed)

	allowed, rejectMethod, err := gate.Check(t.Context(), "1.2.3.4", strings.NewReader(
		`{"method":"eth_getBalance","id":2}`,
	))
	require.NoError(t, err)
	require.False(t, allowed)
	require.Equal(t, "eth_getBalance", rejectMethod)
}

func TestRateLimitGate_CheckProbeLimitRejected(t *testing.T) {
	reg := mustRateLimitRegistry(t, 100, 10)
	gate := NewRateLimitGate(reg, 64, true, "evm")

	padding := strings.Repeat(" ", 50)
	body := `{"params":[` + padding + `],"method":"eth_call","id":1}`

	allowed, rejectMethod, err := gate.Check(t.Context(), "1.2.3.4", strings.NewReader(body))
	require.ErrorIs(t, err, ratelimiter.ErrProbeLimit)
	require.False(t, allowed)
	require.Empty(t, rejectMethod)
}

type trackedBody struct {
	io.Reader
	closed  bool
	drained int64
}

func (b *trackedBody) Read(p []byte) (int, error) {
	n, err := b.Reader.Read(p)
	b.drained += int64(n)
	return n, err
}

func (b *trackedBody) Close() error {
	b.closed = true
	return nil
}

func TestRateLimitMiddleware_ParseErrorRecordsRejectedMetric(t *testing.T) {
	reader := sdkmetric.NewManualReader()
	provider := sdkmetric.NewMeterProvider(sdkmetric.WithReader(reader))
	prev := otel.GetMeterProvider()
	otel.SetMeterProvider(provider)
	t.Cleanup(func() { otel.SetMeterProvider(prev) })

	metrics.requestRejectedCount = must(provider.Meter("evmrpc").Int64Counter(
		"evmrpc_requests_rejected_total",
	))

	reg := mustRateLimitRegistry(t, 100, 10)
	gate := NewRateLimitGate(reg, 0, true, "evm")
	inner := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	})
	h := newRateLimitMiddleware(inner, gate)

	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodPost, "/", strings.NewReader(`{"method":123}`)))
	require.Equal(t, http.StatusBadRequest, rec.Code)

	var rm metricdata.ResourceMetrics
	require.NoError(t, reader.Collect(t.Context(), &rm))
	found := false
	for _, sm := range rm.ScopeMetrics {
		for _, m := range sm.Metrics {
			if m.Name != "evmrpc_requests_rejected_total" {
				continue
			}
			sum := m.Data.(metricdata.Sum[int64])
			require.Equal(t, int64(1), sum.DataPoints[0].Value)
			attrs := sum.DataPoints[0].Attributes.ToSlice()
			require.Contains(t, attrs, attribute.String(rejectReasonKey, rejectReasonUnparseable))
			found = true
		}
	}
	require.True(t, found, "expected evmrpc_requests_rejected_total metric")
}

func TestRateLimitMiddleware_RejectionDrainsAndClosesBody(t *testing.T) {
	reg := mustRateLimitRegistry(t, 0.001, 1)
	gate := NewRateLimitGate(reg, 0, true, "evm")
	inner := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	})
	h := newRateLimitMiddleware(inner, gate)

	fullBody := `{"jsonrpc":"2.0","id":1,"method":"eth_call","params":[]}` + strings.Repeat(" ", 128)

	t.Run("rate limited", func(t *testing.T) {
		req1 := httptest.NewRequest(http.MethodPost, "/", strings.NewReader(fullBody))
		req1.RemoteAddr = "203.0.113.50:1"
		rec1 := httptest.NewRecorder()
		h.ServeHTTP(rec1, req1)
		require.Equal(t, http.StatusOK, rec1.Code)

		tracked := &trackedBody{Reader: strings.NewReader(fullBody)}
		req2 := httptest.NewRequest(http.MethodPost, "/", tracked)
		req2.RemoteAddr = "203.0.113.50:1"
		rec2 := httptest.NewRecorder()
		h.ServeHTTP(rec2, req2)
		require.Equal(t, http.StatusTooManyRequests, rec2.Code)
		require.True(t, tracked.closed)
		require.Equal(t, int64(len(fullBody)), tracked.drained)
	})

	t.Run("parse error", func(t *testing.T) {
		badBody := `{"method":123}` + strings.Repeat("x", 64)
		tracked := &trackedBody{Reader: strings.NewReader(badBody)}
		req := httptest.NewRequest(http.MethodPost, "/", tracked)
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, req)
		require.Equal(t, http.StatusBadRequest, rec.Code)
		require.True(t, tracked.closed)
		require.Equal(t, int64(len(badBody)), tracked.drained)
	})
}

func TestReadBoundedBody_RejectsOversize(t *testing.T) {
	t.Run("exact limit ok", func(t *testing.T) {
		body := `{"method":"eth_call","id":1}`
		buf, err := readBoundedBody(io.NopCloser(strings.NewReader(body)), int64(len(body)))
		require.NoError(t, err)
		require.Equal(t, body, string(buf))
	})

	t.Run("one byte over limit", func(t *testing.T) {
		body := `{"method":"eth_call","id":1}`
		tracked := &trackedBody{Reader: strings.NewReader(body)}
		_, err := readBoundedBody(tracked, int64(len(body)-1))
		require.ErrorIs(t, err, errBodyTooLarge)
		require.True(t, tracked.closed)
		require.Equal(t, int64(len(body)), tracked.drained)
	})
}
