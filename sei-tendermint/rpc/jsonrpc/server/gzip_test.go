package server

import (
	"bufio"
	"bytes"
	"compress/gzip"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"
)

// noAutoDecompressClient reads the raw on-the-wire body. Go's default client
// transparently gunzips responses and would hide the stream bugs we target.
var noAutoDecompressClient = &http.Client{
	Transport: &http.Transport{DisableCompression: true},
}

func expectsCompression(body []byte, setContentLength bool) bool {
	if setContentLength && len(body) < minCompressBytes {
		return false
	}
	return true
}

// gzipDecodeStrict decodes a gzip response and returns an error if any bytes
// remain after the gzip footer — the check that catches early-close corruption.
// Safe to call from any goroutine; use readGzipBodyStrict from the test goroutine.
func gzipDecodeStrict(body io.Reader, want []byte) error {
	gr, err := gzip.NewReader(body)
	if err != nil {
		return fmt.Errorf("gzip.NewReader: %w", err)
	}
	gr.Multistream(false)

	got, err := io.ReadAll(gr)
	if err != nil {
		return fmt.Errorf("gzip decode: %w", err)
	}
	if err := gr.Close(); err != nil {
		return fmt.Errorf("gzip.Reader.Close: %w", err)
	}
	if !bytes.Equal(got, want) {
		return fmt.Errorf("decoded %d bytes, want %d", len(got), len(want))
	}

	var extra [1]byte
	n, err := body.Read(extra[:])
	if n != 0 {
		return fmt.Errorf("trailing byte(s) after gzip stream: %q", extra[:n])
	}
	if err != io.EOF {
		return fmt.Errorf("expected io.EOF after gzip stream, got %v", err)
	}
	return nil
}

// readGzipBodyStrict calls gzipDecodeStrict and fails the test on error.
// Must only be called from the test goroutine; use gzipDecodeStrict from workers.
func readGzipBodyStrict(t *testing.T, body io.Reader, want []byte) {
	t.Helper()
	if err := gzipDecodeStrict(body, want); err != nil {
		t.Fatal(err)
	}
}

func assertGzipRoundTrip(t *testing.T, body []byte, setContentLength bool) {
	t.Helper()

	inner := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if setContentLength {
			w.Header().Set("Content-Length", strconv.Itoa(len(body)))
		}
		if _, err := w.Write(body); err != nil {
			t.Errorf("handler write: %v", err)
		}
	})

	srv := httptest.NewServer(NewGzipHandler(inner))
	defer srv.Close()

	req, err := http.NewRequest(http.MethodGet, srv.URL, nil)
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("Accept-Encoding", "gzip")

	resp, err := noAutoDecompressClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()

	if got := resp.Header.Get("Vary"); !strings.Contains(got, "Accept-Encoding") {
		t.Fatalf("expected Vary to contain Accept-Encoding, got %q", got)
	}

	if expectsCompression(body, setContentLength) {
		if ce := resp.Header.Get("Content-Encoding"); ce != "gzip" {
			t.Fatalf("Content-Encoding = %q, want gzip", ce)
		}
		if setContentLength {
			if cl := resp.Header.Get("Content-Length"); cl == strconv.Itoa(len(body)) {
				t.Fatalf("response retained original uncompressed Content-Length %q", cl)
			}
		}
		compressed, err := io.ReadAll(resp.Body)
		if err != nil {
			t.Fatal(err)
		}
		if len(body) >= 1<<20 && len(compressed) >= len(body) {
			t.Fatalf("compressed body (%d B) not smaller than original (%d B)", len(compressed), len(body))
		}
		readGzipBodyStrict(t, bytes.NewReader(compressed), body)
		return
	}

	if ce := resp.Header.Get("Content-Encoding"); ce != "" {
		t.Fatalf("expected passthrough (no Content-Encoding), got %q", ce)
	}
	got, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(got, body) {
		t.Fatalf("body mismatch")
	}
}

func TestNewGzipHandler_RoundTripSizes(t *testing.T) {
	sizes := []int{0, 1, 4095, 4096, 4097, 1 << 20}

	for _, size := range sizes {
		if testing.Short() && size > 65536 {
			continue
		}
		body := make([]byte, size)
		for i := range body {
			body[i] = byte(i % 251)
		}

		for _, withCL := range []bool{false, true} {
			name := fmt.Sprintf("size=%d/content-length=%v", size, withCL)
			t.Run(name, func(t *testing.T) {
				assertGzipRoundTrip(t, body, withCL)
			})
		}
	}
}

func TestNewGzipHandler_ConcurrentRoundTrip(t *testing.T) {
	srv := httptest.NewServer(NewGzipHandler(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		id := r.URL.Query().Get("id")
		body := concurrentTestPayload(id)
		w.Header().Set("Content-Length", strconv.Itoa(len(body)))
		if _, err := w.Write(body); err != nil {
			t.Errorf("handler write: %v", err)
		}
	})))
	defer srv.Close()

	const workers = 200
	var wg sync.WaitGroup
	wg.Add(workers)

	for i := range workers {
		go func(id int) {
			defer wg.Done()
			idStr := strconv.Itoa(id)
			want := concurrentTestPayload(idStr)

			req, err := http.NewRequest(http.MethodGet, srv.URL+"?id="+idStr, nil)
			if err != nil {
				t.Errorf("worker %d: NewRequest: %v", id, err)
				return
			}
			req.Header.Set("Accept-Encoding", "gzip")

			resp, err := noAutoDecompressClient.Do(req)
			if err != nil {
				t.Errorf("worker %d: Do: %v", id, err)
				return
			}
			defer resp.Body.Close()

			if err := gzipDecodeStrict(resp.Body, want); err != nil {
				t.Errorf("worker %d: %v", id, err)
			}
		}(i)
	}
	wg.Wait()
}

func TestNewGzipHandler_StreamingFlush(t *testing.T) {
	flushed := make(chan struct{})
	proceed := make(chan struct{})
	// handlerErr carries failures from the server handler goroutine where
	// t.Fatal is unsafe; the outer select drains it alongside errCh.
	handlerErr := make(chan error, 1)

	want := []byte("chunk-one-chunk-two")
	inner := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if _, err := w.Write([]byte("chunk-one-")); err != nil {
			t.Errorf("write chunk1: %v", err)
		}
		f, ok := w.(http.Flusher)
		if !ok {
			handlerErr <- fmt.Errorf("handler ResponseWriter must implement http.Flusher")
			return
		}
		f.Flush()
		close(flushed)
		<-proceed
		if _, err := w.Write([]byte("chunk-two")); err != nil {
			t.Errorf("write chunk2: %v", err)
		}
	})

	srv := httptest.NewServer(NewGzipHandler(inner))
	defer srv.Close()

	errCh := make(chan error, 1)
	go func() {
		req, err := http.NewRequest(http.MethodGet, srv.URL, nil)
		if err != nil {
			errCh <- err
			return
		}
		req.Header.Set("Accept-Encoding", "gzip")

		resp, err := noAutoDecompressClient.Do(req)
		if err != nil {
			errCh <- err
			return
		}
		defer resp.Body.Close()

		select {
		case <-flushed:
		case <-time.After(5 * time.Second):
			errCh <- fmt.Errorf("server never flushed first chunk")
			return
		}

		// firstByteCh fires when the reading goroutine receives the first gzip
		// byte, confirming that flushed data arrived at the client before
		// chunk-two is written. Using a notifier avoids relying on a single
		// Read returning ≥1 byte, and lets io.ReadAll collect the full body so
		// no append is needed.
		firstByteCh := make(chan struct{})
		notifier := &firstByteReader{r: resp.Body, notifyCh: firstByteCh}

		bodyC := make(chan []byte, 1)
		readErrC := make(chan error, 1)
		go func() {
			data, err := io.ReadAll(notifier)
			bodyC <- data
			readErrC <- err
		}()

		select {
		case <-firstByteCh:
		case <-time.After(5 * time.Second):
			errCh <- fmt.Errorf("client blocked waiting for bytes after server flush")
			return
		}

		close(proceed)

		data := <-bodyC
		if err := <-readErrC; err != nil {
			errCh <- err
			return
		}
		errCh <- gzipDecodeStrict(bytes.NewReader(data), want)
	}()

	select {
	case err := <-errCh:
		if err != nil {
			t.Fatal(err)
		}
	case err := <-handlerErr:
		t.Fatal(err)
	case <-time.After(10 * time.Second):
		t.Fatal("streaming flush test deadlocked")
	}
}

func concurrentTestPayload(id string) []byte {
	// Unique per request and above minCompressBytes so Content-Length path compresses.
	return []byte(strings.Repeat(id+"_", 512))
}

func TestNewGzipHandler_PassthroughWithoutAcceptEncoding(t *testing.T) {
	body := `{"jsonrpc":"2.0","id":1}`
	inner := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = io.WriteString(w, body)
	})

	req := httptest.NewRequest(http.MethodPost, "/", strings.NewReader("{}"))
	rr := httptest.NewRecorder()

	NewGzipHandler(inner).ServeHTTP(rr, req)

	if got := rr.Header().Get("Vary"); !strings.Contains(got, "Accept-Encoding") {
		t.Fatalf("expected Vary to contain Accept-Encoding on passthrough responses, got %q", got)
	}
	if got := rr.Header().Get("Content-Encoding"); got != "" {
		t.Fatalf("expected no Content-Encoding, got %q", got)
	}
	if rr.Body.String() != body {
		t.Fatalf("body mismatch: %q", rr.Body.String())
	}
}

func TestNewGzipHandler_WebSocketPassthrough(t *testing.T) {
	hijackCalled := false
	inner := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if _, ok := w.(http.Hijacker); ok {
			hijackCalled = true
		}
	})

	req := httptest.NewRequest(http.MethodGet, "/websocket", nil)
	req.Header.Set("Accept-Encoding", "gzip")
	req.Header.Set("Upgrade", "websocket")
	req.Header.Set("Connection", "Upgrade")

	// hijackableRecorder implements http.Hijacker to simulate a real conn;
	// Hijack() returns nils because the test only checks the interface assertion.
	rr := &hijackableRecorder{ResponseRecorder: httptest.NewRecorder()}
	NewGzipHandler(inner).ServeHTTP(rr, req)

	if rr.Header().Get("Content-Encoding") != "" {
		t.Fatal("gzip handler must not compress WebSocket upgrade requests")
	}
	if !hijackCalled {
		t.Fatal("http.Hijacker must be accessible for WebSocket upgrade requests")
	}
}

func TestNewGzipHandler_WebSocketUpgradeTokenList(t *testing.T) {
	hijackCalled := false
	inner := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if _, ok := w.(http.Hijacker); ok {
			hijackCalled = true
		}
	})

	srv := httptest.NewServer(NewGzipHandler(inner))
	defer srv.Close()

	req, err := http.NewRequest(http.MethodGet, srv.URL, nil)
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("Accept-Encoding", "gzip")
	req.Header.Set("Upgrade", "websocket, foo")
	req.Header.Set("Connection", "Upgrade")

	resp, err := noAutoDecompressClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()

	if resp.Header.Get("Content-Encoding") != "" {
		t.Fatal("gzip handler must not compress WebSocket upgrade requests")
	}
	if !hijackCalled {
		t.Fatal("http.Hijacker must be accessible for non-canonical WebSocket upgrade values")
	}
}

func TestGzipResponseWriter_HijackForwarding(t *testing.T) {
	hijackAsserted := false
	inner := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if _, ok := w.(http.Hijacker); ok {
			hijackAsserted = true
		}
	})

	// No Upgrade header → gzipResponseWriter is created (no early return).
	// hijackableRecorder implements http.Hijacker so the wrapper can forward it.
	// This exercises gzipResponseWriter.Hijack() forwarding, not the early-return path.
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req.Header.Set("Accept-Encoding", "gzip")

	rr := &hijackableRecorder{ResponseRecorder: httptest.NewRecorder()}
	NewGzipHandler(inner).ServeHTTP(rr, req)

	if !hijackAsserted {
		t.Fatal("http.Hijacker must be accessible through gzipResponseWriter when no Upgrade header is present")
	}
}

func TestNewGzipHandler_TransferEncodingIdentityOptOut(t *testing.T) {
	body := `{"error":"deadline exceeded"}`
	inner := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Transfer-Encoding", "identity")
		_, _ = io.WriteString(w, body)
	})

	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req.Header.Set("Accept-Encoding", "gzip")
	rr := httptest.NewRecorder()

	NewGzipHandler(inner).ServeHTTP(rr, req)

	if got := rr.Header().Get("Content-Encoding"); got != "" {
		t.Fatalf("expected no Content-Encoding when Transfer-Encoding: identity, got %q", got)
	}
	if rr.Body.String() != body {
		t.Fatalf("body mismatch: %q", rr.Body.String())
	}
}

func TestNewGzipHandler_NoBodyFor204(t *testing.T) {
	inner := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNoContent)
	})

	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req.Header.Set("Accept-Encoding", "gzip")
	rr := httptest.NewRecorder()

	NewGzipHandler(inner).ServeHTTP(rr, req)

	if rr.Code != http.StatusNoContent {
		t.Fatalf("expected 204, got %d", rr.Code)
	}
	if rr.Body.Len() != 0 {
		t.Fatalf("expected empty body for 204, got %d bytes", rr.Body.Len())
	}
	if got := rr.Header().Get("Content-Encoding"); got != "" {
		t.Fatalf("expected no Content-Encoding for 204, got %q", got)
	}
}

func TestNewGzipHandler_PartialContentPassthrough(t *testing.T) {
	body := strings.Repeat("x", minCompressBytes)
	inner := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Range", "bytes 0-1023/4096")
		w.WriteHeader(http.StatusPartialContent)
		_, _ = io.WriteString(w, body)
	})

	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req.Header.Set("Accept-Encoding", "gzip")
	rr := httptest.NewRecorder()

	NewGzipHandler(inner).ServeHTTP(rr, req)

	if rr.Code != http.StatusPartialContent {
		t.Fatalf("expected 206, got %d", rr.Code)
	}
	if got := rr.Header().Get("Content-Encoding"); got != "" {
		t.Fatalf("expected no Content-Encoding for 206, got %q", got)
	}
	if rr.Body.String() != body {
		t.Fatalf("body mismatch: %q", rr.Body.String())
	}
}

func TestNewGzipHandler_ContentRangePassthrough(t *testing.T) {
	body := strings.Repeat("x", minCompressBytes)
	inner := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Range", "bytes 0-1023/4096")
		_, _ = io.WriteString(w, body)
	})

	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req.Header.Set("Accept-Encoding", "gzip")
	rr := httptest.NewRecorder()

	NewGzipHandler(inner).ServeHTTP(rr, req)

	if got := rr.Header().Get("Content-Encoding"); got != "" {
		t.Fatalf("expected no Content-Encoding when Content-Range is set, got %q", got)
	}
	if rr.Body.String() != body {
		t.Fatalf("body mismatch: %q", rr.Body.String())
	}
}

func TestNewGzipHandler_PanicSkipsGzipFooter(t *testing.T) {
	inner := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = io.WriteString(w, strings.Repeat("x", minCompressBytes))
		panic("boom")
	})

	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req.Header.Set("Accept-Encoding", "gzip")
	rr := httptest.NewRecorder()

	defer func() {
		if p := recover(); p != "boom" {
			t.Fatalf("expected panic to propagate, got %v", p)
		}
		// abort() must not append the gzip footer; the truncated stream ends
		// without a valid gzip trailer so it cannot decode as a complete stream.
		if _, err := io.ReadAll(mustGzipReader(t, rr.Body.Bytes())); err == nil {
			t.Fatal("expected truncated (footer-less) gzip stream after panic")
		}
	}()

	NewGzipHandler(inner).ServeHTTP(rr, req)
}

func mustGzipReader(t *testing.T, b []byte) *gzip.Reader {
	t.Helper()
	gr, err := gzip.NewReader(bytes.NewReader(b))
	if err != nil {
		t.Fatalf("gzip.NewReader: %v", err)
	}
	return gr
}

func TestNewGzipHandler_CloseErrorIsSilent(t *testing.T) {
	// gz.Close() is called with _ = to silence the error because headers are
	// already sent and there is no recovery path. This test injects a broken
	// underlying writer (simulating a dropped connection) and verifies that the
	// handler does not panic.
	inner := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = io.WriteString(w, strings.Repeat("x", minCompressBytes))
	})

	rw := &alwaysErrorWriter{header: make(http.Header)}
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req.Header.Set("Accept-Encoding", "gzip")

	NewGzipHandler(inner).ServeHTTP(rw, req) // must not panic

	// init() set Content-Encoding before any write was attempted, so the header
	// must reflect that the gzip path was entered even though every write failed.
	if ce := rw.header.Get("Content-Encoding"); ce != "gzip" {
		t.Fatalf("expected Content-Encoding: gzip to be set before writes failed, got %q", ce)
	}
}

func TestAcceptsGzip_QValueZero(t *testing.T) {
	body := `{"jsonrpc":"2.0","id":1}`
	inner := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = io.WriteString(w, body)
	})

	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req.Header.Set("Accept-Encoding", "gzip;q=0")
	rr := httptest.NewRecorder()

	NewGzipHandler(inner).ServeHTTP(rr, req)

	if got := rr.Header().Get("Content-Encoding"); got != "" {
		t.Fatalf("expected no compression for gzip;q=0, got Content-Encoding: %q", got)
	}
	if rr.Body.String() != body {
		t.Fatalf("body mismatch: %q", rr.Body.String())
	}
}

func TestAcceptsGzip_Wildcard(t *testing.T) {
	body := strings.Repeat("hello world ", 100)
	inner := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = io.WriteString(w, body)
	})

	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req.Header.Set("Accept-Encoding", "*")
	rr := httptest.NewRecorder()

	NewGzipHandler(inner).ServeHTTP(rr, req)

	if got := rr.Header().Get("Content-Encoding"); got != "gzip" {
		t.Fatalf("expected gzip for Accept-Encoding: *, got %q", got)
	}
}

func TestNewGzipHandler_PreExistingContentEncodingPassthrough(t *testing.T) {
	body := "already-compressed-data"
	inner := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Encoding", "gzip")
		_, _ = io.WriteString(w, body)
	})

	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req.Header.Set("Accept-Encoding", "gzip")
	rr := httptest.NewRecorder()

	NewGzipHandler(inner).ServeHTTP(rr, req)

	if rr.Body.String() != body {
		t.Fatalf("body must pass through unmodified, got %q", rr.Body.String())
	}
}

func TestAcceptsGzip_DeflateOnly(t *testing.T) {
	body := `{"jsonrpc":"2.0","id":1}`
	inner := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = io.WriteString(w, body)
	})

	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req.Header.Set("Accept-Encoding", "deflate")
	rr := httptest.NewRecorder()

	NewGzipHandler(inner).ServeHTTP(rr, req)

	if got := rr.Header().Get("Content-Encoding"); got != "" {
		t.Fatalf("expected no compression for Accept-Encoding: deflate, got %q", got)
	}
	if rr.Body.String() != body {
		t.Fatalf("body mismatch: %q", rr.Body.String())
	}
}

func TestAcceptsGzip_MultipleEncodings(t *testing.T) {
	body := strings.Repeat("hello world ", 100)
	inner := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = io.WriteString(w, body)
	})

	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req.Header.Set("Accept-Encoding", "gzip, deflate")
	rr := httptest.NewRecorder()

	NewGzipHandler(inner).ServeHTTP(rr, req)

	if got := rr.Header().Get("Content-Encoding"); got != "gzip" {
		t.Fatalf("expected gzip for Accept-Encoding: gzip, deflate, got %q", got)
	}
}

type hijackableRecorder struct {
	*httptest.ResponseRecorder
}

func (h *hijackableRecorder) Hijack() (net.Conn, *bufio.ReadWriter, error) {
	return nil, nil, nil
}

// firstByteReader wraps an io.Reader and closes notifyCh the first time a
// Read call returns at least one byte. Used in streaming tests to confirm that
// flushed data arrives at the client without consuming the byte separately.
type firstByteReader struct {
	r        io.Reader
	once     sync.Once
	notifyCh chan struct{}
}

func (f *firstByteReader) Read(p []byte) (int, error) {
	n, err := f.r.Read(p)
	if n > 0 {
		f.once.Do(func() { close(f.notifyCh) })
	}
	return n, err
}

// alwaysErrorWriter is a ResponseWriter whose Write always fails, simulating a
// dropped connection. Used to verify gz.Close() errors are silenced safely.
type alwaysErrorWriter struct {
	header http.Header
}

func (a *alwaysErrorWriter) Header() http.Header { return a.header }
func (a *alwaysErrorWriter) WriteHeader(_ int)   {}
func (a *alwaysErrorWriter) Write(_ []byte) (int, error) {
	return 0, errors.New("injected write failure")
}
