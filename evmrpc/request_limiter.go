package evmrpc

import (
	"net/http"

	"golang.org/x/sync/semaphore"
)

// defaultMaxRequestBodyBytes matches github.com/ethereum/go-ethereum/rpc.defaultBodyLimit,
// the per-request HTTP body cap the inner JSON-RPC server applies. It is used when
// max_request_body_bytes is left at 0 ("use the default").
const defaultMaxRequestBodyBytes int64 = 5 * 1024 * 1024

// requestSizeLimiter is an HTTP middleware that bounds peak decode-time memory by
// admitting JSON-RPC requests *before* the body is buffered or decoded. It enforces:
//
//   - maxBody: per-request body cap. Over-Content-Length requests get 413; the body is
//     also wrapped in http.MaxBytesReader so chunked / mis-declared bodies can't exceed it.
//   - budget: a size-weighted global semaphore bounding bytes admitted concurrently;
//     over-budget requests get 429. The reservation is held for the full inner request
//     (not just decode) and trusts the declared Content-Length — conservative by design,
//     so slow or trickled requests can exhaust the budget even when little is buffered.
type requestSizeLimiter struct {
	inner   http.Handler
	maxBody int64               // always > 0 after construction
	budget  *semaphore.Weighted // nil when the global budget is disabled
}

// newRequestSizeLimiter wraps inner with pre-decode admission control. maxBody <= 0
// normalizes to defaultMaxRequestBodyBytes (the per-request cap is always applied —
// 0 means "use the default", never "no cap"). maxConcurrentBytes <= 0 disables the
// global budget. If a positive budget is smaller than maxBody it is raised to maxBody
// so that a single maximum-size request can always be admitted.
func newRequestSizeLimiter(inner http.Handler, maxBody, maxConcurrentBytes int64) http.Handler {
	if maxBody <= 0 {
		maxBody = defaultMaxRequestBodyBytes
	}
	l := &requestSizeLimiter{inner: inner, maxBody: maxBody}
	if maxConcurrentBytes > 0 {
		if maxConcurrentBytes < maxBody {
			maxConcurrentBytes = maxBody
		}
		l.budget = semaphore.NewWeighted(maxConcurrentBytes)
	}
	return l
}

func (l *requestSizeLimiter) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	// Per-request cap on the declared length (header-only, before any body read).
	if r.ContentLength > l.maxBody {
		recordRequestRejected(r.Context(), rejectReasonOversize)
		http.Error(w, "request body too large", http.StatusRequestEntityTooLarge)
		return
	}
	// Backstop for chunked / mis-declared bodies: cap the bytes actually readable.
	r.Body = http.MaxBytesReader(w, r.Body, l.maxBody)

	if l.budget != nil {
		// For requests with a known length Go's HTTP server enforces Content-Length,
		// so it is a sound upper bound on bytes read (and 0 means an empty body that
		// reserves nothing). Chunked / unknown-length requests report ContentLength
		// == -1; reserve the worst case (maxBody) for them. weight is therefore always
		// in [0, maxBody], and the budget is >= maxBody, so the request can always fit.
		weight := r.ContentLength
		if weight < 0 {
			weight = l.maxBody
		}
		if !l.budget.TryAcquire(weight) {
			recordRequestRejected(r.Context(), rejectReasonBusy)
			http.Error(w, "server busy", http.StatusTooManyRequests)
			return
		}
		defer l.budget.Release(weight)
	}

	l.inner.ServeHTTP(w, r)
}
