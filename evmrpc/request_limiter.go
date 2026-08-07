package evmrpc

import (
	"errors"
	"io"
	"net"
	"net/http"
	"os"
	"sync"
	"time"

	"golang.org/x/sync/semaphore"
)

// defaultMaxRequestBodyBytes matches github.com/ethereum/go-ethereum/rpc.defaultBodyLimit,
// the per-request HTTP body cap the inner JSON-RPC server applies. It is used when
// max_request_body_bytes is left at 0 ("use the default").
const defaultMaxRequestBodyBytes int64 = 5 * 1024 * 1024

func effectiveMaxRequestBodyBytes(maxBody int64) int64 {
	if maxBody <= 0 {
		return defaultMaxRequestBodyBytes
	}
	return maxBody
}

// budgetAcquireBatch is the byte step for incremental global-budget accounting.
// Batching limits semaphore contention on large uploads while still bounding what
// a slow/stalled body pins (at most one batch, not the declared Content-Length).
const budgetAcquireBatch int64 = 64 * 1024

// requestSizeLimiter bounds peak decode-time memory by admitting request bodies
// incrementally: maxBody caps a single request (413), budget caps bytes read
// across all requests (429 mid-body), and bodyReadIdleTimeout bounds per-chunk
// stalls (408).
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
	maxBody = effectiveMaxRequestBodyBytes(maxBody)
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
		budgetWrapped = &budgetBody{
			inner:       r.Body,
			budget:      l.budget,
			rc:          http.NewResponseController(w),
			idleTimeout: l.bodyReadIdleTimeout,
			outcome:     &outcome,
		}
		r.Body = budgetWrapped
		// Deferred so a panic anywhere in the inner handler chain (legacy gate,
		// gzip/vhost/cors wrappers) still releases the reserved bytes instead of
		// leaking them from the shared max_concurrent_request_bytes semaphore.
		defer func() {
			_ = budgetWrapped.Close()
			budgetWrapped.release()
		}()
	}

	// cw suppresses the inner handler's own response once outcome is set, so the
	// status/message below always wins over whatever the inner handler wrote.
	cw := &captureResponseWriter{ResponseWriter: w, outcome: &outcome}
	l.inner.ServeHTTP(cw, r)

	if outcome.status != 0 && !cw.wroteHeader {
		recordRequestRejected(r.Context(), outcome.reason)
		// The inner handler chain may have already mutated the shared response header
		// map before a failing body read caused its status/body to be suppressed. For
		// example, the gzip wrapper can set Content-Encoding: gzip. The limiter's
		// replacement http.Error response is written as plain text, not through that
		// gzip wrapper, so remove Content-Encoding to avoid advertising compression
		// that the bytes on the wire do not use.
		w.Header().Del("Content-Encoding")
		http.Error(w, outcome.message, outcome.status)
	}
}

type limiterOutcome struct {
	status  int
	message string
	reason  string
}

// budgetBody wraps r.Body to charge the global byte budget incrementally and to
// enforce a per-chunk body read idle timeout without resetting net/http's
// ReadTimeout before every Read.
type budgetBody struct {
	inner       io.ReadCloser
	budget      *semaphore.Weighted
	rc          *http.ResponseController
	idleTimeout time.Duration
	outcome     *limiterOutcome

	reserved int64 // bytes charged to the global semaphore
	unbilled int64 // bytes read since the last batch charge

	idleMu      sync.Mutex
	idleTimer   *time.Timer
	idleReading bool
}

func (b *budgetBody) Read(p []byte) (int, error) {
	if b.inner == nil {
		return 0, http.ErrBodyReadAfterClose
	}

	b.beginIdleWatch()

	n, err := b.inner.Read(p)

	b.endIdleWatch()

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
			b.clearReadDeadline()
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

func (b *budgetBody) beginIdleWatch() {
	if b.idleTimeout <= 0 {
		return
	}
	b.idleMu.Lock()
	defer b.idleMu.Unlock()
	if b.idleTimer == nil {
		b.idleTimer = time.AfterFunc(b.idleTimeout, b.onIdleTimeout)
	} else {
		b.idleTimer.Reset(b.idleTimeout)
	}
	b.idleReading = true
}

func (b *budgetBody) endIdleWatch() {
	if b.idleTimeout <= 0 {
		return
	}
	b.idleMu.Lock()
	defer b.idleMu.Unlock()
	b.idleReading = false
	if b.idleTimer != nil {
		b.idleTimer.Stop()
	}
}

func (b *budgetBody) onIdleTimeout() {
	b.idleMu.Lock()
	reading := b.idleReading
	rc := b.rc
	b.idleMu.Unlock()
	if !reading || rc == nil {
		return
	}
	// Only touch the socket deadline once the idle period has actually expired.
	// net/http's ReadTimeout stays armed until then.
	_ = rc.SetReadDeadline(time.Now())
}

// clearReadDeadline clears any deadline this wrapper set after an idle timeout.
// It also disarms net/http's read deadline so handler processing after the body
// is consumed is not bounded by the body-read phase.
func (b *budgetBody) clearReadDeadline() {
	if b.rc != nil {
		_ = b.rc.SetReadDeadline(time.Time{})
	}
}

// Close stops the body and charges any trailing unflushed bytes. It does not release
// the budget; requestSizeLimiter does that once, after the inner handler returns.
// If the trailing bytes don't fit the budget (e.g. the inner handler stopped
// reading short of EOF), the request is rejected the same way a mid-read charge
// failure is, so those bytes can't go unbilled and unrejected.
func (b *budgetBody) Close() error {
	if b.inner == nil {
		return nil
	}
	inner := b.inner
	b.inner = nil
	b.endIdleWatch()
	var flushErr error
	if b.budget != nil {
		if flushErr = b.flush(); flushErr != nil {
			b.setOutcome(rejectReasonBudgetMidread, http.StatusTooManyRequests, "server busy")
		}
	}
	err := inner.Close()
	b.clearReadDeadline()
	if err != nil {
		return err
	}
	return flushErr
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

// fail rejects the request with the given status/message, releases the budget,
// and closes the body. If this is the idle-timeout path, onIdleTimeout has set
// a past read deadline on the connection. We intentionally close the body before
// clearing that deadline so any unread bytes that Close drains fail fast instead
// of blocking on a slow/stalled client.
func (b *budgetBody) fail(reason string, status int, message string, readErr error) {
	b.release()
	b.endIdleWatch()
	if b.inner != nil {
		_ = b.inner.Close()
		b.inner = nil
		b.clearReadDeadline()
	}
	b.setOutcome(reason, status, message)
	_ = readErr
}

// setOutcome records the first rejection reason/status/message seen for this
// body, so whichever of fail (mid-read) or Close (trailing bytes at EOF) wins
// the race is what the caller's response reflects.
func (b *budgetBody) setOutcome(reason string, status int, message string) {
	if b.outcome != nil && b.outcome.status == 0 {
		b.outcome.status = status
		b.outcome.message = message
		b.outcome.reason = reason
	}
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
