package evmrpc

import (
	"errors"
	"io"
	"net"
	"net/http"
	"os"
	"time"

	"golang.org/x/sync/semaphore"
)

// defaultMaxRequestBodyBytes matches github.com/ethereum/go-ethereum/rpc.defaultBodyLimit,
// the per-request HTTP body cap the inner JSON-RPC server applies. It is used when
// max_request_body_bytes is left at 0 ("use the default").
const defaultMaxRequestBodyBytes int64 = 5 * 1024 * 1024

// budgetAcquireBatch is the byte step for incremental global-budget accounting.
// Batching limits semaphore contention on large uploads while still bounding what
// a slow/stalled body pins (at most one batch, not the declared Content-Length).
const budgetAcquireBatch int64 = 64 * 1024

// requestSizeLimiter is an HTTP middleware that bounds peak decode-time memory by
// admitting JSON-RPC request bodies incrementally as bytes are read. It enforces:
//
//   - maxBody: per-request body cap. Over-Content-Length requests get 413; the body is
//     also wrapped in http.MaxBytesReader so chunked / mis-declared bodies can't exceed it.
//   - budget: a global semaphore charged in batches as body bytes arrive; over-budget
//     reads get 429 mid-body. Stalled uploads hold at most one batch, not declared size.
//     Charged bytes stay reserved for the whole inner request, not just the read.
//   - bodyReadIdleTimeout: per-chunk idle guard via ResponseController.SetReadDeadline;
//     stalled body reads get 408 and release any budget held so far.
type requestSizeLimiter struct {
	inner               http.Handler
	maxBody             int64 // always > 0 after construction
	budget              *semaphore.Weighted
	bodyReadIdleTimeout time.Duration
}

// newRequestSizeLimiter wraps inner with pre-decode admission control. maxBody <= 0
// normalizes to defaultMaxRequestBodyBytes (the per-request cap is always applied —
// 0 means "use the default", never "no cap"). maxConcurrentBytes <= 0 disables the
// global budget. If a positive budget is smaller than maxBody it is raised to maxBody
// so that a single maximum-size request can always be admitted.
func newRequestSizeLimiter(inner http.Handler, maxBody, maxConcurrentBytes int64, bodyReadIdleTimeout time.Duration) http.Handler {
	if maxBody <= 0 {
		maxBody = defaultMaxRequestBodyBytes
	}
	l := &requestSizeLimiter{
		inner:               inner,
		maxBody:             maxBody,
		bodyReadIdleTimeout: bodyReadIdleTimeout,
	}
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

	var outcome limiterOutcome
	var budgetWrapped *budgetBody
	if l.budget != nil || l.bodyReadIdleTimeout > 0 {
		rc := http.NewResponseController(w)
		budgetWrapped = &budgetBody{
			inner:       r.Body,
			budget:      l.budget,
			rc:          rc,
			idleTimeout: l.bodyReadIdleTimeout,
			outcome:     &outcome,
		}
		r.Body = budgetWrapped
	}

	// cw suppresses the inner handler's own response once outcome is set, so the
	// status/message below always wins over whatever the inner handler wrote.
	cw := &captureResponseWriter{ResponseWriter: w, outcome: &outcome}
	l.inner.ServeHTTP(cw, r)

	if budgetWrapped != nil {
		// Release only after the inner handler chain fully returns, so the budget
		// stays held for the whole request even if an inner handler (e.g. the
		// sei-legacy gate) closes the body early after buffering it.
		_ = budgetWrapped.Close()
		budgetWrapped.release()
	}

	if outcome.status != 0 && !cw.wroteHeader {
		recordRequestRejected(r.Context(), outcome.reason)
		http.Error(w, outcome.message, outcome.status)
	}
}

type limiterOutcome struct {
	status  int
	message string
	reason  string
}

// budgetBody wraps r.Body to charge the global byte budget incrementally and to
// enforce a per-chunk body read idle timeout via ResponseController.
type budgetBody struct {
	inner       io.ReadCloser
	budget      *semaphore.Weighted
	rc          *http.ResponseController
	idleTimeout time.Duration
	outcome     *limiterOutcome

	reserved int64 // bytes charged to the global semaphore
	unbilled int64 // bytes read since the last batch charge
}

func (b *budgetBody) Read(p []byte) (int, error) {
	if b.idleTimeout > 0 && b.rc != nil {
		if err := b.rc.SetReadDeadline(time.Now().Add(b.idleTimeout)); err != nil {
			// ResponseController may be unavailable on exotic ResponseWriters; proceed
			// without the idle guard rather than failing the request.
		}
	}

	n, err := b.inner.Read(p)
	if n > 0 && b.budget != nil {
		if chargeErr := b.charge(int64(n)); chargeErr != nil {
			b.fail(rejectReasonBudgetMidread, http.StatusTooManyRequests, "server busy", chargeErr)
			return n, chargeErr
		}
	}
	if err != nil {
		if errors.Is(err, io.EOF) {
			if flushErr := b.flush(); flushErr != nil {
				b.fail(rejectReasonBudgetMidread, http.StatusTooManyRequests, "server busy", flushErr)
				return n, flushErr
			}
			return n, err
		}
		if isReadIdleTimeout(err) {
			b.fail(rejectReasonSlowBody, http.StatusRequestTimeout, "request timeout", err)
			return n, err
		}
		b.release()
	}
	return n, err
}

// Close stops the body and charges any trailing unflushed bytes. It does not release
// the budget; requestSizeLimiter does that once, after the inner handler returns.
func (b *budgetBody) Close() error {
	if b.inner == nil {
		return nil
	}
	inner := b.inner
	b.inner = nil
	if b.budget != nil {
		_ = b.flush()
	}
	return inner.Close()
}

func (b *budgetBody) charge(n int64) error {
	b.unbilled += n
	for b.unbilled >= budgetAcquireBatch {
		if !b.budget.TryAcquire(budgetAcquireBatch) {
			return errBudgetExhausted
		}
		b.reserved += budgetAcquireBatch
		b.unbilled -= budgetAcquireBatch
	}
	return nil
}

func (b *budgetBody) flush() error {
	if b.unbilled == 0 {
		return nil
	}
	if !b.budget.TryAcquire(b.unbilled) {
		return errBudgetExhausted
	}
	b.reserved += b.unbilled
	b.unbilled = 0
	return nil
}

func (b *budgetBody) release() {
	if b.budget != nil && b.reserved > 0 {
		b.budget.Release(b.reserved)
		b.reserved = 0
	}
	b.unbilled = 0
}

func (b *budgetBody) fail(reason string, status int, message string, readErr error) {
	b.release()
	if b.inner != nil {
		_ = b.inner.Close()
		b.inner = nil
	}
	if b.outcome != nil && b.outcome.status == 0 {
		b.outcome.status = status
		b.outcome.message = message
		b.outcome.reason = reason
	}
	_ = readErr
}

var errBudgetExhausted = errors.New("request byte budget exhausted")

func isReadIdleTimeout(err error) bool {
	if errors.Is(err, os.ErrDeadlineExceeded) {
		return true
	}
	var netErr net.Error
	return errors.As(err, &netErr) && netErr.Timeout()
}

// captureResponseWriter tracks whether the inner handler wrote a response, and once
// outcome is set, suppresses further inner-handler writes so its response can't
// leak past the caller's canonical status/message.
type captureResponseWriter struct {
	http.ResponseWriter
	outcome     *limiterOutcome
	wroteHeader bool
}

func (w *captureResponseWriter) suppressed() bool {
	return w.outcome != nil && w.outcome.status != 0
}

func (w *captureResponseWriter) WriteHeader(statusCode int) {
	if w.suppressed() {
		return
	}
	w.wroteHeader = true
	w.ResponseWriter.WriteHeader(statusCode)
}

func (w *captureResponseWriter) Write(b []byte) (int, error) {
	if w.suppressed() {
		return len(b), nil
	}
	w.wroteHeader = true
	return w.ResponseWriter.Write(b)
}

func (w *captureResponseWriter) Flush() {
	if f, ok := w.ResponseWriter.(http.Flusher); ok {
		f.Flush()
	}
}

func (w *captureResponseWriter) Unwrap() http.ResponseWriter {
	return w.ResponseWriter
}
