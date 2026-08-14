package server

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/sei-protocol/sei-chain/ratelimiter"
	rpctypes "github.com/sei-protocol/sei-chain/sei-tendermint/rpc/jsonrpc/types"
	"github.com/stretchr/testify/require"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	sdkmetric "go.opentelemetry.io/otel/sdk/metric"
	"go.opentelemetry.io/otel/sdk/metric/metricdata"
)

func mustCometBFTRateLimitRegistry(t *testing.T, rps float64, burst int) *ratelimiter.Registry {
	t.Helper()
	reg, err := ratelimiter.New(ratelimiter.Config{RPS: rps, Burst: burst})
	require.NoError(t, err)
	return reg
}

func TestRateLimitMiddleware_POST_AllowsUnderLimit(t *testing.T) {
	reg := mustCometBFTRateLimitRegistry(t, 100, 10)
	gate := NewRateLimitGate(reg, 0, true)

	called := false
	inner := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		called = true
		body, err := io.ReadAll(r.Body)
		require.NoError(t, err)
		require.JSONEq(t, `{"jsonrpc":"2.0","id":1,"method":"status","params":[]}`, string(body))
		w.WriteHeader(http.StatusOK)
	})

	h := NewRateLimitMiddleware(inner, gate)
	req := httptest.NewRequest(http.MethodPost, "/", strings.NewReader(
		`{"jsonrpc":"2.0","id":1,"method":"status","params":[]}`,
	))
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)

	require.Equal(t, http.StatusOK, rec.Code)
	require.True(t, called)
}

func TestRateLimitMiddleware_POST_RejectsAfterBurst(t *testing.T) {
	reg := mustCometBFTRateLimitRegistry(t, 0.001, 1)
	gate := NewRateLimitGate(reg, 0, true)
	inner := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	})
	h := NewRateLimitMiddleware(inner, gate)

	body := `{"jsonrpc":"2.0","id":1,"method":"status","params":[]}`
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
	requireJSONRPCError(t, rec2.Body.Bytes(), int(rpctypes.CodeInternalError), "too many requests")
}

func TestRateLimitMiddleware_POST_PerIPIsolation(t *testing.T) {
	reg := mustCometBFTRateLimitRegistry(t, 0.001, 1)
	gate := NewRateLimitGate(reg, 0, true)
	inner := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusOK) })
	h := NewRateLimitMiddleware(inner, gate)
	body := `{"jsonrpc":"2.0","id":1,"method":"status","params":[]}`

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

func TestRateLimitMiddleware_POST_BatchCountsAllMethods(t *testing.T) {
	reg := mustCometBFTRateLimitRegistry(t, 0.001, 2)
	gate := NewRateLimitGate(reg, 0, true)
	inner := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusOK) })
	h := NewRateLimitMiddleware(inner, gate)

	batch := `[{"method":"status","id":1},{"method":"block","id":2}]`
	req1 := httptest.NewRequest(http.MethodPost, "/", strings.NewReader(batch))
	req1.RemoteAddr = "10.0.0.5:1"
	rec1 := httptest.NewRecorder()
	h.ServeHTTP(rec1, req1)
	require.Equal(t, http.StatusOK, rec1.Code)

	req2 := httptest.NewRequest(http.MethodPost, "/", strings.NewReader(
		`{"jsonrpc":"2.0","id":1,"method":"status","params":[]}`,
	))
	req2.RemoteAddr = "10.0.0.5:1"
	rec2 := httptest.NewRecorder()
	h.ServeHTTP(rec2, req2)
	require.Equal(t, http.StatusTooManyRequests, rec2.Code)
}

func TestRateLimitMiddleware_GET_ExtractsMethodFromPath(t *testing.T) {
	reg := mustCometBFTRateLimitRegistry(t, 0.001, 1)
	gate := NewRateLimitGate(reg, 0, true)
	called := false
	inner := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		called = true
		require.Equal(t, "/status", r.URL.Path)
		w.WriteHeader(http.StatusOK)
	})
	h := NewRateLimitMiddleware(inner, gate)

	req1 := httptest.NewRequest(http.MethodGet, "/status", nil)
	req1.RemoteAddr = "198.51.100.7:1"
	rec1 := httptest.NewRecorder()
	h.ServeHTTP(rec1, req1)
	require.Equal(t, http.StatusOK, rec1.Code)
	require.True(t, called)

	req2 := httptest.NewRequest(http.MethodGet, "/status", nil)
	req2.RemoteAddr = "198.51.100.7:1"
	rec2 := httptest.NewRecorder()
	h.ServeHTTP(rec2, req2)
	require.Equal(t, http.StatusTooManyRequests, rec2.Code)
}

func TestRateLimitMiddleware_GET_PerIPIsolation(t *testing.T) {
	reg := mustCometBFTRateLimitRegistry(t, 0.001, 1)
	gate := NewRateLimitGate(reg, 0, true)
	inner := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusOK) })
	h := NewRateLimitMiddleware(inner, gate)

	reqA := httptest.NewRequest(http.MethodGet, "/block", nil)
	reqA.RemoteAddr = "203.0.113.10:1"
	recA := httptest.NewRecorder()
	h.ServeHTTP(recA, reqA)
	require.Equal(t, http.StatusOK, recA.Code)

	reqA2 := httptest.NewRequest(http.MethodGet, "/block", nil)
	reqA2.RemoteAddr = "203.0.113.10:1"
	recA2 := httptest.NewRecorder()
	h.ServeHTTP(recA2, reqA2)
	require.Equal(t, http.StatusTooManyRequests, recA2.Code)

	reqB := httptest.NewRequest(http.MethodGet, "/block", nil)
	reqB.RemoteAddr = "203.0.113.11:1"
	recB := httptest.NewRecorder()
	h.ServeHTTP(recB, reqB)
	require.Equal(t, http.StatusOK, recB.Code)
}

func TestRateLimitMiddleware_DisabledBypassesGate(t *testing.T) {
	reg := mustCometBFTRateLimitRegistry(t, 0.001, 1)
	gate := NewRateLimitGate(reg, 0, false)
	inner := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusOK) })
	h := NewRateLimitMiddleware(inner, gate)

	body := `{"jsonrpc":"2.0","id":1,"method":"status","params":[]}`
	for i := 0; i < 3; i++ {
		req := httptest.NewRequest(http.MethodPost, "/", strings.NewReader(body))
		req.RemoteAddr = "203.0.113.1:1"
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, req)
		require.Equal(t, http.StatusOK, rec.Code)
	}
}

func TestRateLimitMiddleware_MethodCatalogRateLimited(t *testing.T) {
	inner := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	})

	for _, method := range []string{http.MethodGet, http.MethodHead, http.MethodPost} {
		t.Run(method, func(t *testing.T) {
			reg := mustCometBFTRateLimitRegistry(t, 0.001, 1)
			gate := NewRateLimitGate(reg, 0, true)
			h := NewRateLimitMiddleware(inner, gate)

			req1 := httptest.NewRequest(method, "/", nil)
			req1.RemoteAddr = "203.0.113.1:1"
			rec1 := httptest.NewRecorder()
			h.ServeHTTP(rec1, req1)
			require.Equal(t, http.StatusOK, rec1.Code)

			req2 := httptest.NewRequest(method, "/", nil)
			req2.RemoteAddr = "203.0.113.1:1"
			rec2 := httptest.NewRecorder()
			h.ServeHTTP(rec2, req2)
			require.Equal(t, http.StatusTooManyRequests, rec2.Code)
		})
	}
}

func TestRateLimitMiddleware_GETRootWithBodyRateLimited(t *testing.T) {
	reg := mustCometBFTRateLimitRegistry(t, 0.001, 1)
	gate := NewRateLimitGate(reg, 0, true)
	inner := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	})
	h := NewRateLimitMiddleware(inner, gate)

	body := `{"jsonrpc":"2.0","id":1,"method":"status","params":[]}`
	remote := "203.0.113.99:1"

	req1 := httptest.NewRequest(http.MethodGet, "/", strings.NewReader(body))
	req1.RemoteAddr = remote
	rec1 := httptest.NewRecorder()
	h.ServeHTTP(rec1, req1)
	require.Equal(t, http.StatusOK, rec1.Code)

	req2 := httptest.NewRequest(http.MethodGet, "/", strings.NewReader(body))
	req2.RemoteAddr = remote
	rec2 := httptest.NewRecorder()
	h.ServeHTTP(rec2, req2)
	require.Equal(t, http.StatusTooManyRequests, rec2.Code)
}

func TestRateLimitMiddleware_OPTIONSExempt(t *testing.T) {
	reg := mustCometBFTRateLimitRegistry(t, 0.001, 1)
	gate := NewRateLimitGate(reg, 0, true)
	called := false
	inner := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		called = true
		w.WriteHeader(http.StatusOK)
	})
	h := NewRateLimitMiddleware(inner, gate)

	for i := 0; i < 3; i++ {
		req := httptest.NewRequest(http.MethodOptions, "/status", nil)
		req.RemoteAddr = "203.0.113.1:1"
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, req)
		require.Equal(t, http.StatusOK, rec.Code)
	}
	require.True(t, called)
}

func TestRateLimitMiddleware_POST_RejectionEmitsMetric(t *testing.T) {
	reader := sdkmetric.NewManualReader()
	mp := sdkmetric.NewMeterProvider(sdkmetric.WithReader(reader))
	prev := otel.GetMeterProvider()
	otel.SetMeterProvider(mp)
	t.Cleanup(func() { otel.SetMeterProvider(prev) })

	reg := mustCometBFTRateLimitRegistry(t, 0.001, 1)
	gate := NewRateLimitGate(reg, 0, true)
	inner := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusOK) })
	h := NewRateLimitMiddleware(inner, gate)

	body := `{"jsonrpc":"2.0","id":1,"method":"status","params":[]}`
	req1 := httptest.NewRequest(http.MethodPost, "/", strings.NewReader(body))
	req1.RemoteAddr = "203.0.113.50:1"
	rec1 := httptest.NewRecorder()
	h.ServeHTTP(rec1, req1)
	require.Equal(t, http.StatusOK, rec1.Code)

	req2 := httptest.NewRequest(http.MethodPost, "/", strings.NewReader(body))
	req2.RemoteAddr = "203.0.113.50:1"
	rec2 := httptest.NewRecorder()
	h.ServeHTTP(rec2, req2)
	require.Equal(t, http.StatusTooManyRequests, rec2.Code)

	var rm metricdata.ResourceMetrics
	require.NoError(t, reader.Collect(t.Context(), &rm))
	found := false
	for _, sm := range rm.ScopeMetrics {
		for _, m := range sm.Metrics {
			if m.Name != "rpc_rate_limit_rejected_total" {
				continue
			}
			sum := m.Data.(metricdata.Sum[int64])
			for _, dp := range sum.DataPoints {
				plane := attributeValue(dp.Attributes, "plane")
				if plane != cometbftRateLimitPlane {
					continue
				}
				require.Equal(t, int64(1), dp.Value)
				found = true
			}
		}
	}
	require.True(t, found, "expected rpc_rate_limit_rejected_total{plane=cometbft}")
}

func TestRateLimitMiddleware_POST_OversizeBodyReturns413(t *testing.T) {
	reg := mustCometBFTRateLimitRegistry(t, 100, 10)
	gate := NewRateLimitGate(reg, 64, true)
	inner := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		t.Fatal("inner should not be called")
	})
	h := NewRateLimitMiddleware(inner, gate)

	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodPost, "/", strings.NewReader(strings.Repeat("x", 100))))
	require.Equal(t, http.StatusRequestEntityTooLarge, rec.Code)
	requireJSONRPCError(t, rec.Body.Bytes(), int(rpctypes.CodeInvalidRequest), "request body too large")
}

func TestRateLimitMiddleware_POST_UnlimitedBodyWhenMaxBodyBytesZero(t *testing.T) {
	reg := mustCometBFTRateLimitRegistry(t, 100, 10)
	gate := NewRateLimitGate(reg, 0, true)
	called := false
	inner := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		called = true
		body, err := io.ReadAll(r.Body)
		require.NoError(t, err)
		require.Greater(t, int64(len(body)), DefaultConfig().MaxBodyBytes)
		w.WriteHeader(http.StatusOK)
	})
	h := NewRateLimitMiddleware(inner, gate)

	body := `{"jsonrpc":"2.0","id":1,"method":"broadcast_tx_sync","params":["` + strings.Repeat("a", int(DefaultConfig().MaxBodyBytes)+1) + `"]}`
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/", strings.NewReader(body))
	req.RemoteAddr = "203.0.113.1:1"
	h.ServeHTTP(rec, req)
	require.Equal(t, http.StatusOK, rec.Code)
	require.True(t, called)
}

func TestRateLimitMiddleware_POST_MalformedJSONReturns400(t *testing.T) {
	reg := mustCometBFTRateLimitRegistry(t, 100, 10)
	gate := NewRateLimitGate(reg, 0, true)
	inner := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		t.Fatal("inner should not be called")
	})
	h := NewRateLimitMiddleware(inner, gate)

	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodPost, "/", strings.NewReader(`{"method":123,"id":1}`)))
	require.Equal(t, http.StatusBadRequest, rec.Code)

	var resp rpctypes.RPCResponse
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &resp))
	require.NotNil(t, resp.Error)
	require.Equal(t, int(rpctypes.CodeParseError), resp.Error.Code)
}

func TestRateLimitMiddleware_GETRootServesMethodCatalog(t *testing.T) {
	reg := mustCometBFTRateLimitRegistry(t, 0.001, 1)
	gate := NewRateLimitGate(reg, 0, true)
	h := NewRateLimitMiddleware(testMux(), gate)

	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req.RemoteAddr = "203.0.113.1:1"
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	require.Equal(t, http.StatusOK, rec.Code)
	require.Contains(t, rec.Header().Get("Content-Type"), "text/html")
	require.Contains(t, rec.Body.String(), "Available RPC endpoints")
}

func requireJSONRPCError(t *testing.T, body []byte, wantCode int, wantSubstring string) {
	t.Helper()
	var resp rpctypes.RPCResponse
	require.NoError(t, json.Unmarshal(body, &resp))
	require.NotNil(t, resp.Error)
	require.Equal(t, wantCode, resp.Error.Code)
	require.Contains(t, resp.Error.Data, wantSubstring)
}

func attributeValue(set attribute.Set, key string) string {
	v, _ := set.Value(attribute.Key(key))
	return v.AsString()
}
