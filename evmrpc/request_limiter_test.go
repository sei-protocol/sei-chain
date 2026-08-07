package evmrpc

import (
	"bufio"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

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
		}), 1024, 4096, 0)

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
		}), 100, 0, 0)

		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, newSizedRequest(strings.Repeat("x", 200), 200))

		require.Equal(t, http.StatusRequestEntityTooLarge, rec.Code)
		require.False(t, bodyRead, "oversize body must never reach the inner handler")
	})

	t.Run("zero maxBody uses default cap", func(t *testing.T) {
		l, ok := newRequestSizeLimiter(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {}), 0, 0, 0).(*requestSizeLimiter)
		require.Equal(t, defaultMaxRequestBodyBytes, effectiveMaxRequestBodyBytes(0))
		require.True(t, ok)
		require.Equal(t, defaultMaxRequestBodyBytes, l.maxBody)
	})

	t.Run("chunked body exceeding cap fails on read", func(t *testing.T) {
		var readErr error
		var innerRan bool
		h := newRequestSizeLimiter(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			innerRan = true
			_, readErr = io.ReadAll(r.Body)
		}), 100, 0, 0)

		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, newSizedRequest(strings.Repeat("x", 500), -1))

		require.True(t, innerRan, "inner handler runs; cap is enforced when the body is read")
		require.Error(t, readErr)
	})

	t.Run("raises misconfigured budget to maxBody", func(t *testing.T) {
		const maxBody int64 = 1000
		l, ok := newRequestSizeLimiter(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {}), maxBody, 500, 0).(*requestSizeLimiter)
		require.True(t, ok)
		require.NotNil(t, l.budget)

		ran := false
		h := newRequestSizeLimiter(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			ran = true
			_, _ = io.ReadAll(r.Body)
			w.WriteHeader(http.StatusOK)
		}), maxBody, 500, 0)

		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, newSizedRequest(strings.Repeat("x", int(maxBody)), maxBody))

		require.True(t, ran)
		require.Equal(t, http.StatusOK, rec.Code)
	})

	t.Run("known Content-Length zero body reserves nothing", func(t *testing.T) {
		ran := false
		h := newRequestSizeLimiter(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			ran = true
			_, err := io.ReadAll(r.Body)
			require.NoError(t, err)
			w.WriteHeader(http.StatusOK)
		}), 1024, 1024, 0)

		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, newSizedRequest("", 0))

		require.True(t, ran)
		require.Equal(t, http.StatusOK, rec.Code)
	})
}

func TestRequestSizeLimiter_budgetExhaustionAndRelease(t *testing.T) {
	const maxBody = 1000
	const budget = 1500 // room for exactly one max-size body at a time

	release := make(chan struct{})
	admitted := make(chan struct{}, 1)
	var innerCalls int

	h := newRequestSizeLimiter(
		http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			innerCalls++
			_, err := io.ReadAll(r.Body)
			if err != nil {
				return
			}
			admitted <- struct{}{}
			<-release
			w.WriteHeader(http.StatusOK)
		}),
		maxBody,
		budget,
		0,
	)
	oversizeBody := strings.Repeat("x", maxBody)
	makeRequest := func() *httptest.ResponseRecorder {
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, newSizedRequest(oversizeBody, maxBody))
		return rec
	}

	// First request charges 1000 bytes from the budget, leaving 500 free.
	firstDone := make(chan int, 1)
	go func() { firstDone <- makeRequest().Code }()
	<-admitted

	// Second max-size request fails mid-read once it tries to charge the full body.
	rec2 := makeRequest()
	require.Equal(t, http.StatusTooManyRequests, rec2.Code)
	require.Equal(t, 2, innerCalls, "rejected request reaches inner handler but fails on body read")

	close(release)
	require.Equal(t, http.StatusOK, <-firstDone)

	// Budget was released when the first handler returned; a new request fits again.
	require.Equal(t, http.StatusOK, makeRequest().Code)
	require.Equal(t, 3, innerCalls)
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
		0,
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

func TestRequestSizeLimiter_slowlorisDoesNotPinDeclaredSize(t *testing.T) {
	const maxBody = 1000
	const budget = 1500
	const stallers = 2 // each would have pinned maxBody under the old design

	stallRelease := make(chan struct{})
	var stallersAdmitted int32

	h := newRequestSizeLimiter(
		http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			n := atomic.AddInt32(&stallersAdmitted, 1)
			if n <= stallers {
				buf := make([]byte, 32)
				_, _ = r.Body.Read(buf) // trickle a few bytes, then stall
				<-stallRelease
				return
			}
			_, _ = io.ReadAll(r.Body)
			w.WriteHeader(http.StatusOK)
		}),
		maxBody,
		budget,
		0,
	)

	startStaller := func() {
		go func() {
			rec := httptest.NewRecorder()
			h.ServeHTTP(rec, newSizedRequest(strings.Repeat("x", maxBody), maxBody))
		}()
	}
	for range stallers {
		startStaller()
	}
	require.Eventually(t, func() bool {
		return atomic.LoadInt32(&stallersAdmitted) == stallers
	}, time.Second, 10*time.Millisecond)

	// Small request should still fit: stallers hold at most 32 bytes each, not maxBody.
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, newSizedRequest("ok", 2))
	require.Equal(t, http.StatusOK, rec.Code)

	close(stallRelease)
}

func TestRequestSizeLimiter_incrementalBudgetOnLargeBody(t *testing.T) {
	const maxBody = 200 * 1024
	const budget = maxBody

	inner := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, err := io.ReadAll(r.Body)
		require.NoError(t, err)
		require.Len(t, body, int(maxBody))
		w.WriteHeader(http.StatusOK)
	})

	rec := httptest.NewRecorder()
	h := newRequestSizeLimiter(inner, maxBody, budget, 0)
	h.ServeHTTP(rec, newSizedRequest(strings.Repeat("b", int(maxBody)), maxBody))
	require.Equal(t, http.StatusOK, rec.Code)
}

func TestRequestSizeLimiter_bodyReadIdleTimeout(t *testing.T) {
	idle := 50 * time.Millisecond
	var completed atomic.Bool

	srv := httptest.NewServer(newRequestSizeLimiter(
		http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			_, err := io.ReadAll(r.Body)
			if err == nil {
				completed.Store(true)
				w.WriteHeader(http.StatusOK)
			}
		}),
		1024,
		4096,
		idle,
	))
	t.Cleanup(srv.Close)

	conn, err := net.Dial("tcp", srv.Listener.Addr().String())
	require.NoError(t, err)
	t.Cleanup(func() { _ = conn.Close() })

	host := srv.Listener.Addr().String()
	req := fmt.Sprintf("POST / HTTP/1.1\r\nHost: %s\r\nContent-Length: 100\r\n\r\nx", host)
	_, err = conn.Write([]byte(req))
	require.NoError(t, err)

	time.Sleep(idle + 150*time.Millisecond)

	resp, err := http.ReadResponse(bufio.NewReader(conn), &http.Request{Method: http.MethodPost})
	require.NoError(t, err)
	t.Cleanup(func() { _ = resp.Body.Close() })
	require.Equal(t, http.StatusRequestTimeout, resp.StatusCode)
	require.False(t, completed.Load())
}

func TestRequestSizeLimiter_doesNotExtendServerReadTimeout(t *testing.T) {
	readTimeout := 200 * time.Millisecond
	idleTimeout := 500 * time.Millisecond

	handler := newRequestSizeLimiter(
		http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			_, _ = io.ReadAll(r.Body)
		}),
		1024,
		0,
		idleTimeout,
	)

	srv := httptest.NewUnstartedServer(handler)
	srv.Config.ReadTimeout = readTimeout
	srv.Config.ReadHeaderTimeout = 500 * time.Millisecond
	srv.Start()
	t.Cleanup(srv.Close)

	conn, err := net.Dial("tcp", srv.Listener.Addr().String())
	require.NoError(t, err)
	t.Cleanup(func() { _ = conn.Close() })

	host := srv.Listener.Addr().String()
	start := time.Now()

	// Trickling headers spends time before the limiter runs, but must not push the
	// body-read bound past net/http's request-start ReadTimeout.
	_, err = conn.Write([]byte("POST / HTTP/1.1\r\n"))
	require.NoError(t, err)
	time.Sleep(75 * time.Millisecond)
	_, err = conn.Write([]byte(fmt.Sprintf("Host: %s\r\n", host)))
	require.NoError(t, err)
	time.Sleep(75 * time.Millisecond)
	_, err = conn.Write([]byte("Content-Length: 100\r\n\r\nx"))
	require.NoError(t, err)

	// Idle gap under body_read_idle_timeout, but past the server's ReadTimeout.
	time.Sleep(200 * time.Millisecond)

	require.NoError(t, conn.SetReadDeadline(time.Now().Add(200*time.Millisecond)))
	resp, err := http.ReadResponse(bufio.NewReader(conn), &http.Request{Method: http.MethodPost})
	if err == nil {
		t.Cleanup(func() { _ = resp.Body.Close() })
		require.Equal(t, http.StatusRequestTimeout, resp.StatusCode)
	}
	require.Less(t, time.Since(start), idleTimeout,
		"connection must be cut by server ReadTimeout, not the longer body idle timeout")
}

func TestRequestSizeLimiter_steadySlowUploadSucceeds(t *testing.T) {
	idle := 200 * time.Millisecond
	body := strings.Repeat("z", 512)

	srv := httptest.NewServer(newRequestSizeLimiter(
		http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			got, err := io.ReadAll(r.Body)
			require.NoError(t, err)
			require.Equal(t, body, string(got))
			w.WriteHeader(http.StatusOK)
		}),
		1024,
		4096,
		idle,
	))
	t.Cleanup(srv.Close)

	pr, pw := io.Pipe()
	go func() {
		defer pw.Close()
		chunk := []byte(body)
		for len(chunk) > 0 {
			n := min(64, len(chunk))
			_, _ = pw.Write(chunk[:n])
			chunk = chunk[n:]
			time.Sleep(idle / 4)
		}
	}()

	req, err := http.NewRequest(http.MethodPost, srv.URL, pr)
	require.NoError(t, err)
	req.ContentLength = int64(len(body))

	resp, err := http.DefaultClient.Do(req)
	require.NoError(t, err)
	t.Cleanup(func() { _ = resp.Body.Close() })
	require.Equal(t, http.StatusOK, resp.StatusCode)
}

func TestJWTBeforeRequestSizeLimiter(t *testing.T) {
	var innerCalls int
	secret := []byte("secret")

	inner := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		innerCalls++
		w.WriteHeader(http.StatusOK)
	})
	limiter := newRequestSizeLimiter(inner, 1024, 1024, 0)
	stack := newJWTHandler(secret, limiter)

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/", strings.NewReader("{}"))
	stack.ServeHTTP(rec, req)

	require.Equal(t, http.StatusUnauthorized, rec.Code)
	require.Equal(t, 0, innerCalls, "unauthenticated request must not reach the byte limiter or inner handler")
}

// TestRequestSizeLimiter_budgetOutcomeSurvivesInnerHandlerWrite runs the limiter through
// seiLegacyHTTPGate (always present in production) to check two things the gate can
// break: the limiter's own 429/408 must win over the gate's response, and the budget
// must stay held through the first request's processing, not just its body read.
func TestRequestSizeLimiter_budgetOutcomeSurvivesInnerHandlerWrite(t *testing.T) {
	const maxBody = 1000
	const budget = 1500

	release := make(chan struct{})
	admitted := make(chan struct{}, 1)

	inner := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		admitted <- struct{}{}
		<-release
		w.WriteHeader(http.StatusOK)
	})
	gated := wrapSeiLegacyHTTP(inner, map[string]struct{}{}, maxBody)
	h := newRequestSizeLimiter(gated, maxBody, budget, 0)

	makeRequest := func() *httptest.ResponseRecorder {
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, newSizedRequest(strings.Repeat("x", maxBody), maxBody))
		return rec
	}

	firstDone := make(chan int, 1)
	go func() { firstDone <- makeRequest().Code }()
	<-admitted

	rec := makeRequest()
	require.Equal(t, http.StatusTooManyRequests, rec.Code, "gate's own 400 must not mask the limiter's 429")
	require.NotContains(t, rec.Body.String(), "budget exhausted", "internal error text must not leak to the client")

	close(release)
	require.Equal(t, http.StatusOK, <-firstDone)
}

func BenchmarkRequestSizeLimiter_smallPOST(b *testing.B) {
	body := `{"jsonrpc":"2.0","method":"eth_blockNumber","params":[],"id":1}`
	h := newRequestSizeLimiter(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = io.ReadAll(r.Body)
		w.WriteHeader(http.StatusOK)
	}), defaultMaxRequestBodyBytes, 128*1024*1024, 10*time.Second)

	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, newSizedRequest(body, int64(len(body))))
		if rec.Code != http.StatusOK {
			b.Fatalf("unexpected status %d", rec.Code)
		}
	}
}

func BenchmarkRequestSizeLimiter_maxBodyUpload(b *testing.B) {
	payload := strings.Repeat("x", int(defaultMaxRequestBodyBytes))
	h := newRequestSizeLimiter(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = io.ReadAll(r.Body)
		w.WriteHeader(http.StatusOK)
	}), defaultMaxRequestBodyBytes, 128*1024*1024, 10*time.Second)

	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, newSizedRequest(payload, defaultMaxRequestBodyBytes))
		if rec.Code != http.StatusOK {
			b.Fatalf("unexpected status %d", rec.Code)
		}
	}
}
