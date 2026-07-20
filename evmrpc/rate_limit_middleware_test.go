package evmrpc

import (
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/sei-protocol/sei-chain/ratelimiter"
	"github.com/stretchr/testify/require"
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

func TestRateLimitMiddleware_ProbeLimitPassthrough(t *testing.T) {
	reg := mustRateLimitRegistry(t, 0.001, 1)
	gate := NewRateLimitGate(reg, 64, true, "evm")

	padding := strings.Repeat(" ", 50)
	body := `{"params":[` + padding + `],"method":"eth_call","id":1}`

	called := false
	inner := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		called = true
		_, _ = io.ReadAll(r.Body)
		w.WriteHeader(http.StatusOK)
	})
	h := newRateLimitMiddleware(inner, gate)

	req := httptest.NewRequest(http.MethodPost, "/", strings.NewReader(body))
	req.RemoteAddr = "10.0.0.9:1"
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	require.Equal(t, http.StatusOK, rec.Code)
	require.True(t, called)
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

func TestRateLimitMiddleware_NonPostPassthrough(t *testing.T) {
	reg := mustRateLimitRegistry(t, 0.001, 1)
	gate := NewRateLimitGate(reg, 0, true, "evm")
	called := false
	inner := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		called = true
		w.WriteHeader(http.StatusOK)
	})
	h := newRateLimitMiddleware(inner, gate)

	req := httptest.NewRequest(http.MethodGet, "/", nil)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	require.Equal(t, http.StatusOK, rec.Code)
	require.True(t, called)
}

func TestComposedStack_RateLimitDistinctFromSizeBudget(t *testing.T) {
	const maxBody = 4096
	reg := mustRateLimitRegistry(t, 0.001, 1)
	gate := NewRateLimitGate(reg, 0, true, "evm")
	enabled := BuildSeiLegacyEnabledSet([]string{"sei_getBlockByNumber"})

	body := `{"jsonrpc":"2.0","id":1,"method":"eth_call","params":[]}`
	base := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = io.ReadAll(r.Body)
		w.WriteHeader(http.StatusOK)
	})
	stack := newRateLimitMiddleware(
		newRequestSizeLimiter(wrapSeiLegacyHTTP(base, enabled, maxBody), maxBody, 0),
		gate,
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

func TestRateLimitGate_Check(t *testing.T) {
	reg := mustRateLimitRegistry(t, 0.001, 1)
	gate := NewRateLimitGate(reg, 0, true, "evm")

	allowed, _, passthrough, err := gate.Check(t.Context(), "1.2.3.4", strings.NewReader(
		`{"method":"eth_call","id":1}`,
	))
	require.NoError(t, err)
	require.True(t, allowed)
	require.False(t, passthrough)

	allowed, rejectMethod, passthrough, err := gate.Check(t.Context(), "1.2.3.4", strings.NewReader(
		`{"method":"eth_getBalance","id":2}`,
	))
	require.NoError(t, err)
	require.False(t, allowed)
	require.Equal(t, "eth_getBalance", rejectMethod)
	require.False(t, passthrough)
}
