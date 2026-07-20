package evmrpc

import (
	"bytes"
	"io"
	"net/http"
)

type rateLimitMiddleware struct {
	inner http.Handler
	gate  *RateLimitGate
}

func newRateLimitMiddleware(inner http.Handler, gate *RateLimitGate) http.Handler {
	if gate == nil || !gate.enabled {
		return inner
	}
	return &rateLimitMiddleware{inner: inner, gate: gate}
}

func (m *rateLimitMiddleware) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		m.inner.ServeHTTP(w, r)
		return
	}

	ip := m.gate.registry.IPFromHTTPRequest(r)
	prefix, err := readProbePrefix(r.Body, m.gate.maxProbeBytes)
	if err != nil {
		http.Error(w, "bad request", http.StatusBadRequest)
		return
	}
	r.Body = restoreBody(prefix, r.Body)

	allowed, _, passthrough, checkErr := m.gate.Check(r.Context(), ip, bytes.NewReader(prefix))
	if checkErr != nil {
		http.Error(w, "bad request", http.StatusBadRequest)
		return
	}
	if !allowed && !passthrough {
		recordRequestRejected(r.Context(), rejectReasonRateLimited)
		http.Error(w, "too many requests", http.StatusTooManyRequests)
		return
	}

	m.inner.ServeHTTP(w, r)
}

// readProbePrefix reads up to maxProbeBytes plus one byte from body.
func readProbePrefix(body io.ReadCloser, maxProbeBytes int64) ([]byte, error) {
	lr := &io.LimitedReader{R: body, N: maxProbeBytes + 1}
	return io.ReadAll(lr)
}

func restoreBody(prefix []byte, rest io.Reader) io.ReadCloser {
	return io.NopCloser(io.MultiReader(bytes.NewReader(prefix), rest))
}
