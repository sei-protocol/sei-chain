package server

import (
	"bufio"
	"compress/gzip"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
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
		// Verify the original ResponseWriter is passed through (Hijacker accessible).
		if _, ok := w.(http.Hijacker); ok {
			hijackCalled = true
		}
	})

	req := httptest.NewRequest(http.MethodGet, "/websocket", nil)
	req.Header.Set("Accept-Encoding", "gzip")
	req.Header.Set("Upgrade", "websocket")
	req.Header.Set("Connection", "Upgrade")

	rr := &hijackableRecorder{ResponseRecorder: httptest.NewRecorder()}
	NewGzipHandler(inner).ServeHTTP(rr, req)

	if rr.Header().Get("Content-Encoding") != "" {
		t.Fatal("gzip handler must not compress WebSocket upgrade requests")
	}
	if !hijackCalled {
		t.Fatal("http.Hijacker must be accessible for WebSocket upgrade requests")
	}
}

// hijackableRecorder embeds httptest.ResponseRecorder and implements http.Hijacker
// to simulate a real connection that supports WebSocket upgrade.
type hijackableRecorder struct {
	*httptest.ResponseRecorder
}

func (h *hijackableRecorder) Hijack() (net.Conn, *bufio.ReadWriter, error) {
	return nil, nil, nil
}
