package evmrpc

import (
	"bytes"
	"context"
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
	if isRateLimitExemptHTTPRequest(r) {
		m.inner.ServeHTTP(w, r)
		return
	}

	ip := m.gate.registry.IPFromHTTPRequest(r)
	body, err := readBoundedBody(r.Body, m.gate.maxBodyBytes)
	if err != nil {
		if isRequestBodyTooLarge(err) {
			m.rejectAdmission(r.Context(), w, ip, rejectReasonOversize, http.StatusRequestEntityTooLarge, "request body too large")
			return
		}
		if errors.Is(err, errBudgetExhausted) {
			// Server-side capacity event; outer limiter already owns the response.
			return
		}
		if errors.Is(err, errSlowBody) {
			// Client-caused stall: still charge the per-IP bucket.
			_ = m.gate.chargeAdmissionRejection(r.Context(), ip)
			return
		}
		m.rejectAdmission(r.Context(), w, ip, rejectReasonReadError, http.StatusBadRequest, "bad request")
		return
	}
	r.Body = io.NopCloser(bytes.NewReader(body))

	allowed, rejectMethod, checkErr := m.gate.Check(r.Context(), ip, bytes.NewReader(body))
	if checkErr != nil {
		if ratelimiter.IsBodyTooLarge(checkErr) {
			m.rejectAdmission(r.Context(), w, ip, rejectReasonOversize, http.StatusRequestEntityTooLarge, "request body too large")
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

func (m *rateLimitMiddleware) rejectAdmission(ctx context.Context, w http.ResponseWriter, ip, reason string, status int, msg string) {
	if m.gate.chargeAdmissionRejection(ctx, ip) {
		recordRequestRejected(ctx, rejectReasonRateLimited)
		http.Error(w, "too many requests", http.StatusTooManyRequests)
		return
	}
	recordRequestRejected(ctx, reason)
	http.Error(w, msg, status)
}

// isRateLimitExemptHTTPRequest reports requests that cannot carry a JSON-RPC
// payload and should bypass the per-IP gate. Shapes match go-ethereum's
// rpc.Server health-check fast path and CORS preflight handling.
func isRateLimitExemptHTTPRequest(r *http.Request) bool {
	switch r.Method {
	case http.MethodOptions:
		return true
	case http.MethodGet, http.MethodHead:
		return r.ContentLength == 0 && r.URL.RawQuery == ""
	default:
		return false
	}
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
