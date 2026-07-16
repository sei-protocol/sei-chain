package evmrpc

import (
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"

	"github.com/stretchr/testify/require"
)

func newSizedRequest(body string, contentLength int64) *http.Request {
	r := httptest.NewRequest(http.MethodPost, "/", strings.NewReader(body))
	r.ContentLength = contentLength
	return r
}

// blockUntilRelease admits the request, calls onAdmit, then blocks until release receives a value.
func blockUntilRelease(release <-chan struct{}, onAdmit func()) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if onAdmit != nil {
			onAdmit()
		}
		<-release
		w.WriteHeader(http.StatusOK)
	})
}

func TestRequestSizeLimiter(t *testing.T) {
	t.Run("allows in-budget request", func(t *testing.T) {
		ran := false
		h := newRequestSizeLimiter(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			ran = true
			w.WriteHeader(http.StatusOK)
		}), 1024, 4096)

		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, newSizedRequest("hello", 5))

		require.True(t, ran)
		require.Equal(t, http.StatusOK, rec.Code)
	})

	t.Run("rejects oversize Content-Length before inner handler reads body", func(t *testing.T) {
		var bodyRead bool
		h := newRequestSizeLimiter(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			b, _ := io.ReadAll(r.Body)
			bodyRead = len(b) > 0
		}), 100, 0)

		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, newSizedRequest(strings.Repeat("x", 200), 200))

		require.Equal(t, http.StatusRequestEntityTooLarge, rec.Code)
		require.False(t, bodyRead, "oversize body must never reach the inner handler")
	})

	t.Run("zero maxBody uses default cap", func(t *testing.T) {
		l, ok := newRequestSizeLimiter(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {}), 0, 0).(*requestSizeLimiter)
		require.True(t, ok)
		require.Equal(t, defaultMaxRequestBodyBytes, l.maxBody)
	})

	t.Run("chunked body exceeding cap fails on read", func(t *testing.T) {
		var readErr error
		var innerRan bool
		h := newRequestSizeLimiter(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			innerRan = true
			_, readErr = io.ReadAll(r.Body)
		}), 100, 0)

		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, newSizedRequest(strings.Repeat("x", 500), -1))

		require.True(t, innerRan, "inner handler runs; cap is enforced when the body is read")
		require.Error(t, readErr)
	})

	t.Run("raises misconfigured budget to maxBody", func(t *testing.T) {
		const maxBody int64 = 1000
		l, ok := newRequestSizeLimiter(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {}), maxBody, 500).(*requestSizeLimiter)
		require.True(t, ok)
		require.NotNil(t, l.budget)

		ran := false
		h := newRequestSizeLimiter(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			ran = true
			w.WriteHeader(http.StatusOK)
		}), maxBody, 500)

		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, newSizedRequest(strings.Repeat("x", int(maxBody)), maxBody))

		require.True(t, ran)
		require.Equal(t, http.StatusOK, rec.Code)
	})
}

func TestRequestSizeLimiter_budgetExhaustionAndRelease(t *testing.T) {
	const maxBody = 1000
	const budget = 1500 // room for exactly one max-size request at a time

	release := make(chan struct{})
	admitted := make(chan struct{}, 1)
	var innerCalls int

	h := newRequestSizeLimiter(
		blockUntilRelease(release, func() {
			innerCalls++
			admitted <- struct{}{}
		}),
		maxBody,
		budget,
	)
	oversizeBody := strings.Repeat("x", maxBody)
	makeRequest := func() *httptest.ResponseRecorder {
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, newSizedRequest(oversizeBody, maxBody))
		return rec
	}

	// First request acquires weight=1000, leaving 500 < 1000.
	firstDone := make(chan int, 1)
	go func() { firstDone <- makeRequest().Code }()
	<-admitted //wait until the first request is processed

	// Second request needs 1000 but only 500 remains.
	rec2 := makeRequest()
	require.Equal(t, http.StatusTooManyRequests, rec2.Code)
	require.Equal(t, 1, innerCalls, "rejected request must not reach inner handler")

	close(release) // closed channel unblocks the first request and any later admitted ones
	require.Equal(t, http.StatusOK, <-firstDone)

	// Budget was released; a new request fits again.
	require.Equal(t, http.StatusOK, makeRequest().Code)
	require.Equal(t, 2, innerCalls)
}

func TestRequestSizeLimiter_budgetDisabled(t *testing.T) {
	const maxBody = 1000
	release := make(chan struct{})
	var admitted sync.WaitGroup
	admitted.Add(2)

	h := newRequestSizeLimiter(
		blockUntilRelease(release, admitted.Done),
		maxBody,
		0, // budget disabled
	)
	oversizeBody := strings.Repeat("x", maxBody)

	codes := make(chan int, 2)
	for range 2 {
		go func() {
			rec := httptest.NewRecorder()
			h.ServeHTTP(rec, newSizedRequest(oversizeBody, maxBody))
			codes <- rec.Code
		}()
	}
	admitted.Wait()
	close(release)

	require.Equal(t, http.StatusOK, <-codes)
	require.Equal(t, http.StatusOK, <-codes)
}
