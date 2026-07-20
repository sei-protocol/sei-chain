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
	origBody := r.Body
	prefix, err := readProbePrefix(origBody, m.gate.maxProbeBytes)
	if err != nil {
		discardAndCloseBody(origBody)
		recordRequestRejected(r.Context(), rejectReasonUnparseable)
		http.Error(w, "bad request", http.StatusBadRequest)
		return
	}
	r.Body = restoreBody(prefix, origBody)

	allowed, _, checkErr := m.gate.Check(r.Context(), ip, bytes.NewReader(prefix))
	if checkErr != nil {
		discardAndCloseBody(r.Body)
		recordRequestRejected(r.Context(), rejectReasonUnparseable)
		http.Error(w, "bad request", http.StatusBadRequest)
		return
	}
	if !allowed {
		discardAndCloseBody(r.Body)
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

// restoredBody replays a bounded prefix then the unread remainder of the
// original body. Close drains any leftover bytes and closes the original so
// rejected requests do not leave connections undrained.
type restoredBody struct {
	io.Reader
	orig io.ReadCloser
}

func restoreBody(prefix []byte, orig io.ReadCloser) io.ReadCloser {
	return &restoredBody{
		Reader: io.MultiReader(bytes.NewReader(prefix), orig),
		orig:   orig,
	}
}

func (b *restoredBody) Close() error {
	_, _ = io.Copy(io.Discard, b.Reader)
	return b.orig.Close()
}

func discardAndCloseBody(body io.ReadCloser) {
	if body == nil {
		return
	}
	_, _ = io.Copy(io.Discard, body)
	_ = body.Close()
}
