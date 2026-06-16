package server

import (
	"bufio"
	"compress/gzip"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"
)

func TestNewGzipHandler_CompressesWhenAccepted(t *testing.T) {
	body := strings.Repeat("hello world ", 100)
	inner := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, body)
	})

	req := httptest.NewRequest(http.MethodGet, "/status", nil)
	req.Header.Set("Accept-Encoding", "gzip")
	rr := httptest.NewRecorder()

	NewGzipHandler(inner).ServeHTTP(rr, req)

	if got := rr.Header().Get("Content-Encoding"); got != "gzip" {
		t.Fatalf("expected Content-Encoding: gzip, got %q", got)
	}
	if got := rr.Header().Get("Vary"); !strings.Contains(got, "Accept-Encoding") {
		t.Fatalf("expected Vary to contain Accept-Encoding, got %q", got)
	}
	r, err := gzip.NewReader(rr.Body)
	if err != nil {
		t.Fatalf("response body is not valid gzip: %v", err)
	}
	decoded, err := io.ReadAll(r)
	if err != nil {
		t.Fatalf("gzip decode error: %v", err)
	}
	if string(decoded) != body {
		t.Fatalf("decoded body mismatch")
	}
	if rr.Body.Len() >= len(body) {
		t.Fatalf("compressed size (%d) not smaller than original (%d)", rr.Body.Len(), len(body))
	}
}

func TestNewGzipHandler_PassthroughWithoutAcceptEncoding(t *testing.T) {
	body := `{"jsonrpc":"2.0","id":1}`
	inner := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = io.WriteString(w, body)
	})

	req := httptest.NewRequest(http.MethodPost, "/", strings.NewReader("{}"))
	rr := httptest.NewRecorder()

	NewGzipHandler(inner).ServeHTTP(rr, req)

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

func TestNewGzipHandler_ContentLengthEarlyClose(t *testing.T) {
	body := strings.Repeat("hello world ", 100)
	inner := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Length", strconv.Itoa(len(body)))
		_, _ = io.WriteString(w, body)
	})

	req := httptest.NewRequest(http.MethodGet, "/status", nil)
	req.Header.Set("Accept-Encoding", "gzip")
	rr := httptest.NewRecorder()

	NewGzipHandler(inner).ServeHTTP(rr, req)

	if got := rr.Header().Get("Content-Encoding"); got != "gzip" {
		t.Fatalf("expected Content-Encoding: gzip, got %q", got)
	}
	// Content-Length must be removed since the compressed length differs.
	if got := rr.Header().Get("Content-Length"); got != "" {
		t.Fatalf("expected Content-Length to be stripped, got %q", got)
	}
	gr, err := gzip.NewReader(rr.Body)
	if err != nil {
		t.Fatalf("response body is not valid gzip: %v", err)
	}
	decoded, err := io.ReadAll(gr)
	if err != nil {
		t.Fatalf("gzip decode error: %v", err)
	}
	if string(decoded) != body {
		t.Fatalf("decoded body mismatch")
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

func TestNewGzipHandler_Flush(t *testing.T) {
	flushed := false
	inner := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = io.WriteString(w, "chunk")
		if f, ok := w.(http.Flusher); ok {
			f.Flush()
		}
	})

	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req.Header.Set("Accept-Encoding", "gzip")

	fr := &flushRecorder{ResponseRecorder: httptest.NewRecorder(), onFlush: func() { flushed = true }}
	NewGzipHandler(inner).ServeHTTP(fr, req)

	if !flushed {
		t.Fatal("Flush was not propagated to the underlying ResponseWriter")
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

// flushRecorder wraps httptest.ResponseRecorder and implements http.Flusher,
// calling onFlush when Flush is invoked.
type flushRecorder struct {
	*httptest.ResponseRecorder
	onFlush func()
}

func (f *flushRecorder) Flush() {
	f.onFlush()
	f.ResponseRecorder.Flush()
}
