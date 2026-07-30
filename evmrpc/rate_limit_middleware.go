package evmrpc

import (
	"bytes"
	"errors"
	"io"
	"net/http"

	"github.com/sei-protocol/sei-chain/ratelimiter"
)

var errBodyTooLarge = errors.New("request body too large")

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
	body, err := readBoundedBody(r.Body, m.gate.maxBodyBytes)
	if err != nil {
		if isRequestBodyTooLarge(err) {
			recordRequestRejected(r.Context(), rejectReasonOversize)
			http.Error(w, "request body too large", http.StatusRequestEntityTooLarge)
			return
		}
		http.Error(w, "bad request", http.StatusBadRequest)
		return
	}
	r.Body = io.NopCloser(bytes.NewReader(body))

	allowed, rejectMethod, checkErr := m.gate.Check(r.Context(), ip, bytes.NewReader(body))
	if checkErr != nil {
		if ratelimiter.IsBodyTooLarge(checkErr) {
			recordRequestRejected(r.Context(), rejectReasonOversize)
			http.Error(w, "request body too large", http.StatusRequestEntityTooLarge)
			return
		}
		recordRequestRejected(r.Context(), rejectReasonUnparseable)
		http.Error(w, "bad request", http.StatusBadRequest)
		return
	}
	if !allowed {
		logger.Debug("rate limit rejected request", "ip", ip, "method", rejectMethod, "plane", m.gate.plane)
		recordRequestRejected(r.Context(), rejectReasonRateLimited)
		http.Error(w, "too many requests", http.StatusTooManyRequests)
		return
	}

	m.inner.ServeHTTP(w, r)
}

// readBoundedBody reads the entire request body, rejecting when it exceeds
// maxBytes. The original body is always drained and closed.
func readBoundedBody(body io.ReadCloser, maxBytes int64) ([]byte, error) {
	if body == nil {
		return nil, errors.New("missing request body")
	}
	defer func() {
		_, _ = io.Copy(io.Discard, body)
		_ = body.Close()
	}()

	lr := &io.LimitedReader{R: body, N: maxBytes + 1}
	buf, err := io.ReadAll(lr)
	if err != nil {
		return nil, err
	}
	if int64(len(buf)) > maxBytes {
		return nil, errBodyTooLarge
	}
	return buf, nil
}

// isRequestBodyTooLarge reports whether err indicates the request body exceeded
// maxBodyBytes. The outer requestSizeLimiter wraps r.Body in http.MaxBytesReader,
// which returns *http.MaxBytesError; readBoundedBody returns errBodyTooLarge when
// the gate runs without that wrapper.
func isRequestBodyTooLarge(err error) bool {
	if errors.Is(err, errBodyTooLarge) {
		return true
	}
	var maxErr *http.MaxBytesError
	return errors.As(err, &maxErr)
}
