package evmrpc

import (
	"bytes"
	"compress/gzip"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// gzipHandler is a minimal middleware that gzip-compresses the response body
// when the client sends Accept-Encoding: gzip.  It mirrors the real middleware
// that sits between seiLegacyHTTPGate and the inner JSON-RPC handler.
func gzipHandler(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !strings.Contains(r.Header.Get("Accept-Encoding"), "gzip") {
			next.ServeHTTP(w, r)
			return
		}
		w.Header().Set("Content-Encoding", "gzip")
		gz := gzip.NewWriter(w)
		defer gz.Close()
		next.ServeHTTP(&testGzipResponseWriter{ResponseWriter: w, gz: gz}, r)
	})
}

type testGzipResponseWriter struct {
	http.ResponseWriter
	gz *gzip.Writer
}

func (g *testGzipResponseWriter) Write(b []byte) (int, error) { return g.gz.Write(b) }

// echoHandler replies with a valid JSON-RPC response echoing the method name.
var echoHandler = http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
	body, _ := io.ReadAll(r.Body)
	w.Header().Set("Content-Type", "application/json")
	// Determine if batch or single.
	trim := bytes.TrimSpace(body)
	if len(trim) > 0 && trim[0] == '[' {
		var msgs []json.RawMessage
		_ = json.Unmarshal(trim, &msgs)
		var out []json.RawMessage
		for _, raw := range msgs {
			out = append(out, makeEchoResp(raw))
		}
		_ = json.NewEncoder(w).Encode(out)
		return
	}
	resp := makeEchoResp(trim)
	_, _ = w.Write(resp)
})

func makeEchoResp(raw json.RawMessage) json.RawMessage {
	var msg struct {
		ID     json.RawMessage `json:"id"`
		Method string          `json:"method"`
	}
	_ = json.Unmarshal(raw, &msg)
	id := msg.ID
	if len(id) == 0 {
		id = json.RawMessage(`null`)
	}
	resp, _ := json.Marshal(map[string]interface{}{
		"jsonrpc": "2.0",
		"id":      id,
		"result":  msg.Method,
	})
	return resp
}

// allowAll is a permissive allowlist that lets every sei_* method through.
var allowAll = map[string]struct{}{
	"sei_getFilterLogs": {},
	"sei_someMethod":    {},
}

// postJSON builds an *http.Request with the given JSON body and Accept-Encoding: gzip.
func postJSON(t *testing.T, body string) *http.Request {
	t.Helper()
	req := httptest.NewRequest(http.MethodPost, "/", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept-Encoding", "gzip")
	return req
}

// readGzipBody decompresses a gzip response body and returns the plain bytes.
func readGzipBody(t *testing.T, rec *httptest.ResponseRecorder) []byte {
	t.Helper()
	gr, err := gzip.NewReader(rec.Body)
	if err != nil {
		t.Fatalf("gzip.NewReader: %v  (body hex: %x)", err, rec.Body.Bytes())
	}
	defer gr.Close()
	out, err := io.ReadAll(gr)
	if err != nil {
		t.Fatalf("reading gzip body: %v", err)
	}
	return out
}

// --------------------------------------------------------------------------
// Concern 1: batch path must not corrupt gzip for non-gated methods
// --------------------------------------------------------------------------

func TestHandleBatch_NonGatedMethods_GzipIntact(t *testing.T) {
	// Wrap: gate → gzip → echo.  Without the fix the gate's recorder
	// captures the gzip bytes and replays them raw → broken gzip frame.
	gate := wrapSeiLegacyHTTP(gzipHandler(echoHandler), allowAll)

	body := `[
		{"jsonrpc":"2.0","id":1,"method":"eth_blockNumber"},
		{"jsonrpc":"2.0","id":2,"method":"eth_chainId"}
	]`
	rec := httptest.NewRecorder()
	gate.ServeHTTP(rec, postJSON(t, body))

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rec.Code)
	}

	plain := readGzipBody(t, rec) // will fail here if gzip is corrupted

	var arr []json.RawMessage
	if err := json.Unmarshal(plain, &arr); err != nil {
		t.Fatalf("unmarshal batch response: %v\nbody: %s", err, plain)
	}
	if len(arr) != 2 {
		t.Fatalf("got %d responses, want 2", len(arr))
	}
}

// --------------------------------------------------------------------------
// Concern 1 (single): non-gated single request must not corrupt gzip
// --------------------------------------------------------------------------

func TestHandleSingle_NonGatedMethod_GzipIntact(t *testing.T) {
	gate := wrapSeiLegacyHTTP(gzipHandler(echoHandler), allowAll)

	body := `{"jsonrpc":"2.0","id":1,"method":"eth_call"}`
	rec := httptest.NewRecorder()
	gate.ServeHTTP(rec, postJSON(t, body))

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rec.Code)
	}
	plain := readGzipBody(t, rec)

	var resp map[string]interface{}
	if err := json.Unmarshal(plain, &resp); err != nil {
		t.Fatalf("unmarshal: %v\nbody: %s", err, plain)
	}
	if resp["result"] != "eth_call" {
		t.Fatalf("result = %v, want eth_call", resp["result"])
	}
}

// --------------------------------------------------------------------------
// Gated method still gets deprecation header (single)
// --------------------------------------------------------------------------

func TestHandleSingle_GatedMethod_DeprecationHeader(t *testing.T) {
	gate := wrapSeiLegacyHTTP(gzipHandler(echoHandler), allowAll)

	body := `{"jsonrpc":"2.0","id":1,"method":"sei_getFilterLogs"}`
	rec := httptest.NewRecorder()
	gate.ServeHTTP(rec, postJSON(t, body))

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rec.Code)
	}
	if h := rec.Header().Get(SeiLegacyDeprecationHTTPHeader); h == "" {
		t.Fatal("missing deprecation header on gated method")
	}
}

// --------------------------------------------------------------------------
// Gated method still gets deprecation header (batch)
// --------------------------------------------------------------------------

func TestHandleBatch_GatedMethod_DeprecationHeader(t *testing.T) {
	gate := wrapSeiLegacyHTTP(gzipHandler(echoHandler), allowAll)

	body := `[
		{"jsonrpc":"2.0","id":1,"method":"sei_getFilterLogs"},
		{"jsonrpc":"2.0","id":2,"method":"eth_blockNumber"}
	]`
	rec := httptest.NewRecorder()
	gate.ServeHTTP(rec, postJSON(t, body))

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rec.Code)
	}
	if h := rec.Header().Get(SeiLegacyDeprecationHTTPHeader); h == "" {
		t.Fatal("missing deprecation header when batch contains gated method")
	}
}

// --------------------------------------------------------------------------
// Blocked method returns gate error (single)
// --------------------------------------------------------------------------

func TestHandleSingle_BlockedMethod_ReturnsError(t *testing.T) {
	// Empty allowlist → all sei_* methods are blocked.
	gate := wrapSeiLegacyHTTP(gzipHandler(echoHandler), map[string]struct{}{})

	body := `{"jsonrpc":"2.0","id":1,"method":"sei_getFilterLogs"}`
	rec := httptest.NewRecorder()
	gate.ServeHTTP(rec, postJSON(t, body))

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rec.Code)
	}
	var resp map[string]interface{}
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if resp["error"] == nil {
		t.Fatal("expected JSON-RPC error for blocked method")
	}
}
