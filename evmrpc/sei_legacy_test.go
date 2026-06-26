package evmrpc

import (
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/ethereum/go-ethereum/rpc"
	"github.com/stretchr/testify/require"
)

func TestConstants(t *testing.T) {
	require.Equal(t, -32600, invalidRequestCode)
	require.Equal(t, -32603, internalErrorCode)
}

func TestBuildSeiLegacyEnabledSet_Empty(t *testing.T) {
	s := BuildSeiLegacyEnabledSet(nil)
	if len(s) != 0 {
		t.Fatalf("expected empty set, got %v", s)
	}
}

func TestBuildSeiLegacyEnabledSet_InitDefaults(t *testing.T) {
	s := BuildSeiLegacyEnabledSet([]string{"sei_getSeiAddress", "sei_getEVMAddress", "sei_getCosmosTx"})
	if len(s) != 3 {
		t.Fatalf("want 3 entries, got %d", len(s))
	}
	if _, ok := s["sei_getBlockByNumber"]; ok {
		t.Fatal("block should be off")
	}
}

func TestBuildSeiLegacyEnabledSet_Extra(t *testing.T) {
	s := BuildSeiLegacyEnabledSet([]string{"sei_getBlockByNumber", "SEI_GETBLOCKRECEIPTS"})
	if _, ok := s["sei_getBlockByNumber"]; !ok {
		t.Fatal("expected sei_getBlockByNumber")
	}
	if _, ok := s["sei_getBlockReceipts"]; !ok {
		t.Fatal("expected case-insensitive match")
	}
}

func TestSeiLegacyGateError_DisabledWhenEmptyAllowlist(t *testing.T) {
	err := seiLegacyGateError("sei_getBlockByNumber", BuildSeiLegacyEnabledSet(nil))
	if err == nil {
		t.Fatal("expected error")
	}
	var withData rpc.DataError
	if !errors.As(err, &withData) {
		t.Fatalf("want rpc.DataError, got %T", err)
	}
	if withData.ErrorData() != "legacy_sei_deprecated" {
		t.Fatalf("error data: %v", withData.ErrorData())
	}
	msg := err.Error()
	if !strings.Contains(msg, "not enabled") {
		t.Fatalf("message: %s", msg)
	}
}

func TestSeiLegacyGateError_AllowedWhenListed(t *testing.T) {
	enabled := BuildSeiLegacyEnabledSet([]string{"sei_getBlockByNumber"})
	err := seiLegacyGateError("Sei_GetBlockByNumber", enabled)
	if err != nil {
		t.Fatalf("unexpected: %v", err)
	}
}

func TestSeiLegacyGateError_UnknownSeiNamespaceFailsClosed(t *testing.T) {
	enabled := BuildSeiLegacyEnabledSet([]string{"sei_getBlockByNumber"})
	err := seiLegacyGateError("sei_notARealRegisteredMethod", enabled)
	if err == nil {
		t.Fatal("expected error for unknown sei_* method when allowlist is active")
	}
	var withData rpc.DataError
	if !errors.As(err, &withData) {
		t.Fatalf("want rpc.DataError, got %T", err)
	}
	if withData.ErrorData() != "legacy_sei_deprecated" {
		t.Fatalf("error data: %v", withData.ErrorData())
	}
}

func TestSeiLegacyGateError_Sei2BlockedUnlessListed(t *testing.T) {
	err := seiLegacyGateError("sei2_getBlockByNumber", BuildSeiLegacyEnabledSet(nil))
	if err == nil {
		t.Fatal("expected error")
	}
	enabled := BuildSeiLegacyEnabledSet([]string{"sei2_getBlockByNumber"})
	if err := seiLegacyGateError("SEI2_GETBLOCKBYNUMBER", enabled); err != nil {
		t.Fatalf("unexpected: %v", err)
	}
}

func TestBuildSeiLegacyEnabledSet_IncludesSei2(t *testing.T) {
	s := BuildSeiLegacyEnabledSet([]string{"sei2_getBlockReceipts"})
	if _, ok := s["sei2_getBlockReceipts"]; !ok {
		t.Fatalf("got %v", s)
	}
	if _, ok := s["sei_getBlockReceipts"]; ok {
		t.Fatal("sei_* should not be enabled from sei2_ name only")
	}
}

func TestSeiLegacyGateError_NilAllowlistUngated(t *testing.T) {
	err := seiLegacyGateError("sei_getBlockByNumber", nil)
	if err != nil {
		t.Fatal(err)
	}
}

func TestWrapSeiLegacyHTTP_UnknownSeiMethodBlocked(t *testing.T) {
	inner := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		t.Fatal("inner should not run for unknown sei_* method")
	})
	enabled := BuildSeiLegacyEnabledSet([]string{"sei_getBlockByNumber"})
	h := wrapSeiLegacyHTTP(inner, enabled, 0)
	req := httptest.NewRequest(http.MethodPost, "/", strings.NewReader(
		`{"jsonrpc":"2.0","id":1,"method":"sei_futureHypotheticalMethod","params":[]}`))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	var resp map[string]interface{}
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatal(err)
	}
	if resp["error"] == nil {
		t.Fatalf("expected error, got %s", rec.Body.String())
	}
	errObj, _ := resp["error"].(map[string]interface{})
	if errObj["data"] != "legacy_sei_deprecated" {
		t.Fatalf("error data: %v", errObj)
	}
}

func TestWrapSeiLegacyHTTP_BlocksDisabledMethod(t *testing.T) {
	inner := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		t.Fatal("inner should not run")
	})
	h := wrapSeiLegacyHTTP(inner, BuildSeiLegacyEnabledSet(nil), 0)
	req := httptest.NewRequest(http.MethodPost, "/", strings.NewReader(
		`{"jsonrpc":"2.0","id":1,"method":"sei_getBlockByNumber","params":["0x1",false]}`))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	var resp map[string]interface{}
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatal(err)
	}
	errObj, _ := resp["error"].(map[string]interface{})
	if errObj == nil {
		t.Fatalf("expected error, got %s", rec.Body.String())
	}
	if errObj["data"] != "legacy_sei_deprecated" {
		t.Fatalf("error data: %v", errObj)
	}
	if rec.Code != http.StatusOK {
		t.Fatalf("want HTTP 200, got %d", rec.Code)
	}
}

// TestWrapSeiLegacyHTTP_RaisedBodyLimitNotTruncated guards against the legacy gate
// truncating a request body at the old fixed 5MiB cap when the configured
// max_request_body_bytes is raised above it. With a raised limit the full body must
// reach the inner JSON-RPC handler intact.
func TestWrapSeiLegacyHTTP_RaisedBodyLimitNotTruncated(t *testing.T) {
	const maxBody = 8 * 1024 * 1024 // raised above the 5MiB go-ethereum default
	var gotLen int
	inner := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		b, err := io.ReadAll(r.Body)
		if err != nil {
			t.Fatalf("inner read: %v", err)
		}
		gotLen = len(b)
		_, _ = w.Write([]byte(`{"jsonrpc":"2.0","id":1,"result":"0x1"}`))
	})
	enabled := BuildSeiLegacyEnabledSet([]string{"sei_getBlockByNumber"})
	h := wrapSeiLegacyHTTP(inner, enabled, maxBody)

	// Allowed gated method with a padded param pushing the body well past 5MiB.
	pad := strings.Repeat("a", 6*1024*1024)
	body := `{"jsonrpc":"2.0","id":1,"method":"sei_getBlockByNumber","params":["` + pad + `"]}`
	if len(body) <= seiLegacyHTTPDefault5MiB {
		t.Fatalf("test body %d must exceed 5MiB to exercise truncation", len(body))
	}
	req := httptest.NewRequest(http.MethodPost, "/", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)

	if gotLen != len(body) {
		t.Fatalf("inner saw %d bytes, want full body %d (truncated at gate)", gotLen, len(body))
	}
}

func TestWrapSeiLegacyHTTP_OverLimitBodyRejectedNotTruncated(t *testing.T) {
	const maxBody = 1024
	innerCalled := false
	inner := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		innerCalled = true
	})
	enabled := BuildSeiLegacyEnabledSet([]string{"sei_getBlockByNumber"})
	h := wrapSeiLegacyHTTP(inner, enabled, maxBody)

	// Body exceeds maxBody. The gate must reject with 413 rather than silently
	// truncating to maxBody and forwarding to the inner handler.
	pad := strings.Repeat("a", maxBody)
	body := `{"jsonrpc":"2.0","id":1,"method":"sei_getBlockByNumber","params":["` + pad + `"]}`
	if int64(len(body)) <= maxBody {
		t.Fatalf("test body %d must exceed maxBody %d", len(body), maxBody)
	}
	req := httptest.NewRequest(http.MethodPost, "/", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)

	if rec.Code != http.StatusRequestEntityTooLarge {
		t.Fatalf("got status %d, want 413", rec.Code)
	}
	if innerCalled {
		t.Fatal("inner handler must not be invoked for an over-limit body")
	}
}

func TestWrapSeiLegacyHTTP_BodyExactlyAtLimitForwarded(t *testing.T) {
	const maxBody = 4096
	var gotLen int
	inner := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		b, err := io.ReadAll(r.Body)
		if err != nil {
			t.Fatalf("inner read: %v", err)
		}
		gotLen = len(b)
		_, _ = w.Write([]byte(`{"jsonrpc":"2.0","id":1,"result":"0x1"}`))
	})
	enabled := BuildSeiLegacyEnabledSet([]string{"sei_getBlockByNumber"})
	h := wrapSeiLegacyHTTP(inner, enabled, maxBody)

	prefix := `{"jsonrpc":"2.0","id":1,"method":"sei_getBlockByNumber","params":["`
	suffix := `"]}`
	pad := strings.Repeat("a", maxBody-len(prefix)-len(suffix))
	body := prefix + pad + suffix
	if int64(len(body)) != maxBody {
		t.Fatalf("test body %d must equal maxBody %d", len(body), maxBody)
	}
	req := httptest.NewRequest(http.MethodPost, "/", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("got status %d, want 200 for body exactly at limit", rec.Code)
	}
	if gotLen != len(body) {
		t.Fatalf("inner saw %d bytes, want full body %d", gotLen, len(body))
	}
}

// TestComposedStack_OverLimitRejectedConsistently exercises the full production wrapping
// (requestSizeLimiter -> seiLegacyHTTPGate -> base) so the gate and the limiter are tested in
// composition, not just in isolation. Both an over-limit chunked body (ContentLength == -1,
// which slips past the limiter's header-only check) and an over-limit declared-length body must
// be rejected with 413 without reaching the inner handler, and an at-limit body must pass.
func TestComposedStack_OverLimitRejectedConsistently(t *testing.T) {
	const maxBody = 1024
	prefix := `{"jsonrpc":"2.0","id":1,"method":"sei_getBlockByNumber","params":["`
	suffix := `"]}`
	enabled := BuildSeiLegacyEnabledSet([]string{"sei_getBlockByNumber"})

	mkBody := func(total int) string {
		return prefix + strings.Repeat("a", total-len(prefix)-len(suffix)) + suffix
	}

	cases := []struct {
		name          string
		bodyLen       int
		chunked       bool // ContentLength == -1
		wantStatus    int
		wantForwarded bool
	}{
		{"chunked oversize", maxBody + 64, true, http.StatusRequestEntityTooLarge, false},
		{"declared oversize", maxBody + 64, false, http.StatusRequestEntityTooLarge, false},
		{"at limit forwarded", maxBody, false, http.StatusOK, true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			forwarded := false
			base := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				forwarded = true
				_, _ = io.ReadAll(r.Body)
				_, _ = w.Write([]byte(`{"jsonrpc":"2.0","id":1,"result":"0x1"}`))
			})
			stack := newRequestSizeLimiter(wrapSeiLegacyHTTP(base, enabled, maxBody), maxBody, 0)

			req := httptest.NewRequest(http.MethodPost, "/", strings.NewReader(mkBody(tc.bodyLen)))
			req.Header.Set("Content-Type", "application/json")
			if tc.chunked {
				req.ContentLength = -1
			}
			rec := httptest.NewRecorder()
			stack.ServeHTTP(rec, req)

			if rec.Code != tc.wantStatus {
				t.Fatalf("status = %d, want %d (body %q)", rec.Code, tc.wantStatus, rec.Body.String())
			}
			if forwarded != tc.wantForwarded {
				t.Fatalf("forwarded to inner = %v, want %v", forwarded, tc.wantForwarded)
			}
		})
	}
}

const seiLegacyHTTPDefault5MiB = 5 * 1024 * 1024

func TestWrapSeiLegacyHTTP_AllowedMethodPassthroughAndDeprecationHeader(t *testing.T) {
	called := false
	inner := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		called = true
		_, _ = w.Write([]byte(`{"jsonrpc":"2.0","id":1,"result":{"number":"0x1"}}`))
	})
	enabled := BuildSeiLegacyEnabledSet([]string{"sei_getBlockByNumber"})
	h := wrapSeiLegacyHTTP(inner, enabled, 0)
	req := httptest.NewRequest(http.MethodPost, "/", strings.NewReader(
		`{"jsonrpc":"2.0","id":1,"method":"sei_getBlockByNumber","params":["latest",false]}`))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if !called {
		t.Fatal("inner should run for allowlisted method")
	}
	var resp map[string]interface{}
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatal(err)
	}
	res, _ := resp["result"].(map[string]interface{})
	if res == nil {
		t.Fatalf("expected result object: %s", rec.Body.String())
	}
	if res["number"] != "0x1" {
		t.Fatalf("inner result should be unchanged: %+v", res)
	}
	if rec.Header().Get(SeiLegacyDeprecationHTTPHeader) == "" {
		t.Fatal("expected deprecation HTTP header on allowlisted sei_* response")
	}
}

func TestWrapSeiLegacyHTTP_StringResultPassthrough(t *testing.T) {
	inner := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(`{"jsonrpc":"2.0","id":1,"result":"bech32addr"}`))
	})
	enabled := BuildSeiLegacyEnabledSet([]string{"sei_getSeiAddress"})
	h := wrapSeiLegacyHTTP(inner, enabled, 0)
	req := httptest.NewRequest(http.MethodPost, "/", strings.NewReader(
		`{"jsonrpc":"2.0","id":1,"method":"sei_getSeiAddress","params":["0x0000000000000000000000000000000000000001"]}`))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	var resp map[string]interface{}
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatal(err)
	}
	res, _ := resp["result"].(string)
	if res != "bech32addr" {
		t.Fatalf("result: %v", resp)
	}
	if rec.Header().Get(SeiLegacyDeprecationHTTPHeader) == "" {
		t.Fatal("expected deprecation HTTP header")
	}
}

func TestWrapSeiLegacyHTTP_Sei2BlockedWhenNotAllowlisted(t *testing.T) {
	inner := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		t.Fatal("inner should not run")
	})
	h := wrapSeiLegacyHTTP(inner, BuildSeiLegacyEnabledSet(nil), 0)
	req := httptest.NewRequest(http.MethodPost, "/", strings.NewReader(
		`{"jsonrpc":"2.0","id":1,"method":"sei2_getBlockByNumber","params":["latest",false]}`))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	var resp map[string]interface{}
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatal(err)
	}
	if resp["error"] == nil {
		t.Fatalf("expected error: %s", rec.Body.String())
	}
}

func TestWrapSeiLegacyHTTP_Sei2AllowlistedPassthroughAndHeader(t *testing.T) {
	called := false
	inner := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		called = true
		_, _ = w.Write([]byte(`{"jsonrpc":"2.0","id":1,"result":{"number":"0x1"}}`))
	})
	enabled := BuildSeiLegacyEnabledSet([]string{"sei2_getBlockByNumber"})
	h := wrapSeiLegacyHTTP(inner, enabled, 0)
	req := httptest.NewRequest(http.MethodPost, "/", strings.NewReader(
		`{"jsonrpc":"2.0","id":1,"method":"sei2_getBlockByNumber","params":["latest",false]}`))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if !called {
		t.Fatal("inner should run")
	}
	if rec.Header().Get(SeiLegacyDeprecationHTTPHeader) == "" {
		t.Fatal("expected deprecation header")
	}
}

func TestWrapSeiLegacyHTTP_EthPassthrough(t *testing.T) {
	called := false
	inner := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		called = true
		_, _ = w.Write([]byte(`{"jsonrpc":"2.0","id":1,"result":"0x1"}`))
	})
	h := wrapSeiLegacyHTTP(inner, BuildSeiLegacyEnabledSet(nil), 0)
	req := httptest.NewRequest(http.MethodPost, "/", strings.NewReader(
		`{"jsonrpc":"2.0","id":1,"method":"eth_blockNumber","params":[]}`))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if !called {
		t.Fatal("eth_* should reach inner")
	}
	if rec.Header().Get(SeiLegacyDeprecationHTTPHeader) != "" {
		t.Fatal("eth_* should not set legacy deprecation header")
	}
}

func TestWrapSeiLegacyHTTP_BatchTrailingNonObjectDoesNotBypassGate(t *testing.T) {
	inner := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		t.Fatal("inner must not run when all batch slots are answered by the gate (blocked + invalid)")
	})
	h := wrapSeiLegacyHTTP(inner, BuildSeiLegacyEnabledSet(nil), 0)
	body := `[{"jsonrpc":"2.0","id":1,"method":"sei_getBlockByNumber","params":["0x1",false]},42]`
	req := httptest.NewRequest(http.MethodPost, "/", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	var batch []map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &batch); err != nil {
		t.Fatal(err)
	}
	if len(batch) != 2 {
		t.Fatalf("want 2 entries, got %d: %s", len(batch), rec.Body.String())
	}
	err0, _ := batch[0]["error"].(map[string]any)
	if err0 == nil || err0["data"] != "legacy_sei_deprecated" {
		t.Fatalf("slot 0 should be legacy gate error: %+v", batch[0])
	}
	err1, _ := batch[1]["error"].(map[string]any)
	if err1 == nil || int(err1["code"].(float64)) != invalidRequestCode {
		t.Fatalf("slot 1 should be JSON-RPC invalid request (-32600): %+v", batch[1])
	}
}

func TestWrapSeiLegacyHTTP_BatchLeadingNonObjectDoesNotBypassGate(t *testing.T) {
	inner := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		t.Fatal("inner must not run when all batch slots are answered by the gate")
	})
	h := wrapSeiLegacyHTTP(inner, BuildSeiLegacyEnabledSet(nil), 0)
	body := `[42,{"jsonrpc":"2.0","id":1,"method":"sei_getBlockByNumber","params":["0x1",false]}]`
	req := httptest.NewRequest(http.MethodPost, "/", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	var batch []map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &batch); err != nil {
		t.Fatal(err)
	}
	if len(batch) != 2 {
		t.Fatalf("want 2 entries, got %d", len(batch))
	}
	err0, _ := batch[0]["error"].(map[string]any)
	if err0 == nil || int(err0["code"].(float64)) != invalidRequestCode {
		t.Fatalf("slot 0 should be -32600 invalid request: %+v", batch[0])
	}
	err1, _ := batch[1]["error"].(map[string]any)
	if err1 == nil || err1["data"] != "legacy_sei_deprecated" {
		t.Fatalf("slot 1 should be legacy gate error: %+v", batch[1])
	}
}

func TestWrapSeiLegacyHTTP_BatchMixed(t *testing.T) {
	inner := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		b, _ := io.ReadAll(r.Body)
		if !strings.Contains(string(b), "eth_chainId") {
			t.Fatalf("unexpected forward body: %s", b)
		}
		_, _ = w.Write([]byte(`[{"jsonrpc":"2.0","id":0,"result":"0x1"}]`))
	})
	h := wrapSeiLegacyHTTP(inner, BuildSeiLegacyEnabledSet(nil), 0)
	body := `[{"jsonrpc":"2.0","id":1,"method":"sei_getBlockByNumber","params":[]},{"jsonrpc":"2.0","id":2,"method":"eth_chainId","params":[]}]`
	req := httptest.NewRequest(http.MethodPost, "/", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	var batch []map[string]interface{}
	if err := json.Unmarshal(rec.Body.Bytes(), &batch); err != nil {
		t.Fatal(err)
	}
	if len(batch) != 2 {
		t.Fatalf("want 2 results, got %d", len(batch))
	}
	if batch[0]["error"] == nil {
		t.Fatal("first should be error")
	}
	if batch[1]["result"] == nil {
		t.Fatal("second should be result")
	}
	err0, _ := batch[0]["error"].(map[string]interface{})
	if err0["data"] != "legacy_sei_deprecated" {
		t.Fatalf("blocked error data: %+v", err0)
	}
}

func TestWrapSeiLegacyHTTP_BatchInvalidNonObjectNotForwarded(t *testing.T) {
	inner := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		b, err := io.ReadAll(r.Body)
		if err != nil {
			t.Fatal(err)
		}
		var fwd []map[string]any
		if err := json.Unmarshal(b, &fwd); err != nil || len(fwd) != 1 {
			t.Fatalf("inner should receive exactly one forwarded call, got err=%v body=%s", err, b)
		}
		if fwd[0]["method"] != "eth_chainId" {
			t.Fatalf("unexpected forward: %+v", fwd[0])
		}
		_, _ = w.Write([]byte(`[{"jsonrpc":"2.0","id":0,"result":"0xaa"}]`))
	})
	h := wrapSeiLegacyHTTP(inner, BuildSeiLegacyEnabledSet(nil), 0)
	body := `[{"jsonrpc":"2.0","id":1,"method":"eth_chainId","params":[]},42]`
	req := httptest.NewRequest(http.MethodPost, "/", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	var batch []map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &batch); err != nil {
		t.Fatal(err)
	}
	if len(batch) != 2 {
		t.Fatalf("want 2 entries, got %d", len(batch))
	}
	if batch[0]["result"] != "0xaa" {
		t.Fatalf("slot 0: %+v", batch[0])
	}
	err1, _ := batch[1]["error"].(map[string]any)
	if err1 == nil || int(err1["code"].(float64)) != invalidRequestCode {
		t.Fatalf("slot 1 want -32600: %+v", batch[1])
	}
}

func TestWrapSeiLegacyHTTP_BatchNotificationOmittedFromMergedResponse(t *testing.T) {
	inner := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		b, _ := io.ReadAll(r.Body)
		var fwd []map[string]any
		if err := json.Unmarshal(b, &fwd); err != nil || len(fwd) != 2 {
			t.Fatalf("inner should receive notification + call, got %q err=%v", b, err)
		}
		if _, ok := fwd[0]["id"]; ok {
			t.Fatalf("first forwarded item should be notification (no id): %#v", fwd[0])
		}
		if fwd[1]["id"] == nil {
			t.Fatalf("second item should have id: %#v", fwd[1])
		}
		_, _ = w.Write([]byte(`[{"jsonrpc":"2.0","id":1,"result":"0xaa"}]`))
	})
	h := wrapSeiLegacyHTTP(inner, BuildSeiLegacyEnabledSet(nil), 0)
	body := `[
		{"jsonrpc":"2.0","method":"eth_chainId","params":[]},
		{"jsonrpc":"2.0","id":1,"method":"eth_chainId","params":[]}
	]`
	req := httptest.NewRequest(http.MethodPost, "/", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	var batch []map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &batch); err != nil {
		t.Fatal(err)
	}
	if len(batch) != 1 {
		t.Fatalf("want 1 response (notification omitted), got %d: %s", len(batch), rec.Body.String())
	}
	if batch[0]["result"] != "0xaa" {
		t.Fatalf("expected inner result: %+v", batch[0])
	}
}

func TestWrapSeiLegacyHTTP_BatchNullIDIsNotNotification(t *testing.T) {
	inner := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		b, _ := io.ReadAll(r.Body)
		var fwd []map[string]any
		if err := json.Unmarshal(b, &fwd); err != nil || len(fwd) != 2 {
			t.Fatalf("inner should receive 2 calls, got err=%v body=%s", err, b)
		}
		if _, ok := fwd[0]["id"]; !ok {
			t.Fatalf("first item must include id (null): %#v", fwd[0])
		}
		if fwd[0]["id"] != nil {
			t.Fatalf("first item id should decode as nil: %#v", fwd[0])
		}
		_, _ = w.Write([]byte(`[
			{"jsonrpc":"2.0","id":null,"result":"a"},
			{"jsonrpc":"2.0","id":1,"result":"b"}
		]`))
	})
	h := wrapSeiLegacyHTTP(inner, BuildSeiLegacyEnabledSet(nil), 0)
	body := `[
		{"jsonrpc":"2.0","id":null,"method":"eth_chainId","params":[]},
		{"jsonrpc":"2.0","id":1,"method":"eth_chainId","params":[]}
	]`
	req := httptest.NewRequest(http.MethodPost, "/", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	var batch []map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &batch); err != nil {
		t.Fatal(err)
	}
	if len(batch) != 2 {
		t.Fatalf("want 2 responses (null id is not a notification), got %d: %s", len(batch), rec.Body.String())
	}
	if batch[0]["result"] != "a" || batch[1]["result"] != "b" {
		t.Fatalf("unexpected merge order or results: %+v, %+v", batch[0], batch[1])
	}
}

// TestWrapSeiLegacyHTTP_BatchTwoNullIDsDifferentResults is the regression test for the bug where multiple
// "id":null requests in a slow-path batch caused appendMergeFailure to replace ALL responses (including
// unrelated unique-ID ones) with internal errors. The fix assigns unique synthetic IDs before forwarding so
// idToIdx is always collision-free.
func TestWrapSeiLegacyHTTP_BatchTwoNullIDsDifferentResults(t *testing.T) {
	inner := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		b, _ := io.ReadAll(r.Body)
		var fwd []map[string]any
		// 4 items: synthID-0, synthID-1, notification (no id), synthID-2.
		if err := json.Unmarshal(b, &fwd); err != nil || len(fwd) != 4 {
			t.Fatalf("inner should receive 4 forwarded calls, got err=%v body=%s", err, b)
		}
		// First two: null-ID requests rewritten to synthetic IDs 0 and 1.
		for j := 0; j < 2; j++ {
			id, ok := fwd[j]["id"].(float64)
			if !ok {
				t.Fatalf("item %d: expected synthetic integer id, got %#v", j, fwd[j]["id"])
			}
			if int(id) != j {
				t.Fatalf("item %d: expected synthetic id %d, got %v", j, j, id)
			}
		}
		// Third: notification forwarded as-is — must have no "id" key.
		if _, ok := fwd[2]["id"]; ok {
			t.Fatalf("item 2: notification must not have id, got %#v", fwd[2]["id"])
		}
		// Fourth: regular request rewritten to synthetic ID 2.
		if id, ok := fwd[3]["id"].(float64); !ok || int(id) != 2 {
			t.Fatalf("item 3: expected synthetic id 2, got %#v", fwd[3]["id"])
		}
		// Inner returns no response for the notification; three responses for the rest.
		_, _ = w.Write([]byte(`[
			{"jsonrpc":"2.0","id":0,"result":"foo"},
			{"jsonrpc":"2.0","id":1,"result":"bar"},
			{"jsonrpc":"2.0","id":2,"result":"baz"}
		]`))
	})
	h := wrapSeiLegacyHTTP(inner, BuildSeiLegacyEnabledSet(nil), 0)
	// Two null-ID requests + a notification + a regular request + a blocked sei_ method.
	// The blocked method triggers the slow path; the notification must be omitted from output.
	body := `[
		{"jsonrpc":"2.0","id":null,"method":"eth_chainId","params":[]},
		{"jsonrpc":"2.0","id":null,"method":"eth_gasPrice","params":[]},
		{"jsonrpc":"2.0","method":"eth_chainId","params":[]},
		{"jsonrpc":"2.0","id":1,"method":"eth_chainId","params":[]},
		{"jsonrpc":"2.0","id":2,"method":"sei_getBlockByNumber","params":[]}
	]`
	req := httptest.NewRequest(http.MethodPost, "/", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	var batch []map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &batch); err != nil {
		t.Fatalf("unmarshal: %v, body: %s", err, rec.Body.String())
	}
	// 5 input slots: notification is omitted → 4 output entries.
	if len(batch) != 4 {
		t.Fatalf("want 4 responses (notification omitted), got %d: %s", len(batch), rec.Body.String())
	}
	if batch[0]["result"] != "foo" {
		t.Fatalf("slot 0: want result 'foo', got %+v", batch[0])
	}
	if batch[0]["id"] != nil {
		t.Fatalf("slot 0: want id null, got %v", batch[0]["id"])
	}
	if batch[1]["result"] != "bar" {
		t.Fatalf("slot 1: want result 'bar', got %+v", batch[1])
	}
	if batch[1]["id"] != nil {
		t.Fatalf("slot 1: want id null, got %v", batch[1]["id"])
	}
	if batch[2]["result"] != "baz" {
		t.Fatalf("slot 2: want result 'baz', got %+v", batch[2])
	}
	if id, _ := batch[2]["id"].(float64); int(id) != 1 {
		t.Fatalf("slot 2: want id 1, got %v", batch[2]["id"])
	}
	errObj, _ := batch[3]["error"].(map[string]any)
	if errObj == nil || errObj["data"] != "legacy_sei_deprecated" {
		t.Fatalf("slot 3: want legacy gate error, got %+v", batch[3])
	}
}

func TestWrapSeiLegacyHTTP_BatchLeadingNotificationMergedWithBlockedCall(t *testing.T) {
	inner := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		b, _ := io.ReadAll(r.Body)
		var fwd []map[string]any
		if err := json.Unmarshal(b, &fwd); err != nil || len(fwd) != 1 {
			t.Fatalf("inner should receive only the notification, got %q", b)
		}
		// Inner stack: no JSON objects for an all-notification forward (JSON-RPC 2.0).
		_, _ = w.Write([]byte(`[]`))
	})
	h := wrapSeiLegacyHTTP(inner, BuildSeiLegacyEnabledSet(nil), 0)
	body := `[
		{"jsonrpc":"2.0","method":"eth_chainId","params":[]},
		{"jsonrpc":"2.0","id":7,"method":"sei_getBlockByNumber","params":[]}
	]`
	req := httptest.NewRequest(http.MethodPost, "/", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	var batch []map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &batch); err != nil {
		t.Fatal(err)
	}
	if len(batch) != 1 {
		t.Fatalf("want 1 entry (notification omitted + blocked error), got %d: %s", len(batch), rec.Body.String())
	}
	err0, _ := batch[0]["error"].(map[string]any)
	if err0 == nil || err0["data"] != "legacy_sei_deprecated" {
		t.Fatalf("want legacy gate error: %+v", batch[0])
	}
}

func TestWrapSeiLegacyHTTP_BatchInvalidThenNotificationOneResponse(t *testing.T) {
	inner := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		t.Fatal("inner must not run")
	})
	h := wrapSeiLegacyHTTP(inner, BuildSeiLegacyEnabledSet(nil), 0)
	// Second item is a notification (no id) but gated sei_* — blocked and not forwarded; still no JSON-RPC response for it.
	body := `[42,{"jsonrpc":"2.0","method":"sei_getBlockByNumber","params":[]}]`
	req := httptest.NewRequest(http.MethodPost, "/", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	var batch []map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &batch); err != nil {
		t.Fatal(err)
	}
	if len(batch) != 1 {
		t.Fatalf("want 1 entry (-32600 only; notification omitted), got %d: %s", len(batch), rec.Body.String())
	}
	err0, _ := batch[0]["error"].(map[string]any)
	if err0 == nil || int(err0["code"].(float64)) != invalidRequestCode {
		t.Fatalf("want -32600: %+v", batch[0])
	}
}

func TestWrapSeiLegacyHTTP_BatchAllBlockedNotificationsEmptyHTTPBody(t *testing.T) {
	inner := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		t.Fatal("inner must not run")
	})
	h := wrapSeiLegacyHTTP(inner, BuildSeiLegacyEnabledSet(nil), 0)
	body := `[
		{"jsonrpc":"2.0","method":"sei_getBlockByNumber","params":[]},
		{"jsonrpc":"2.0","method":"sei_getSeiAddress","params":["0x0000000000000000000000000000000000000001"]}
	]`
	req := httptest.NewRequest(http.MethodPost, "/", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("status: %d", rec.Code)
	}
	if len(bytes.TrimSpace(rec.Body.Bytes())) != 0 {
		t.Fatalf("JSON-RPC 2.0 forbids empty batch array; want empty body, got %q", rec.Body.String())
	}
}

func TestWrapSeiLegacyHTTP_BatchSingleBlockedNotificationEmptyHTTPBody(t *testing.T) {
	inner := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		t.Fatal("inner must not run: blocked notification must not be forwarded")
	})
	h := wrapSeiLegacyHTTP(inner, BuildSeiLegacyEnabledSet(nil), 0)
	// Single notification (no id) for a gated method: blocked AND a notification,
	// so no response entry and nothing forwarded — expect empty HTTP body, not [].
	body := `[{"jsonrpc":"2.0","method":"sei_getBlockByNumber","params":[]}]`
	req := httptest.NewRequest(http.MethodPost, "/", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("status: %d", rec.Code)
	}
	if len(bytes.TrimSpace(rec.Body.Bytes())) != 0 {
		t.Fatalf("JSON-RPC 2.0 forbids empty batch array; want empty body, got %q", rec.Body.String())
	}
}

func TestWrapSeiLegacyHTTP_BatchInnerReorderedByID(t *testing.T) {
	inner := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		// Inner returns results permuted vs forwarded request order (using synthetic IDs 0, 1).
		_, _ = w.Write([]byte(`[
			{"jsonrpc":"2.0","id":1,"result":"second"},
			{"jsonrpc":"2.0","id":0,"result":"first"}
		]`))
	})
	h := wrapSeiLegacyHTTP(inner, BuildSeiLegacyEnabledSet(nil), 0)
	body := `[
		{"jsonrpc":"2.0","id":1,"method":"sei_getBlockByNumber","params":[]},
		{"jsonrpc":"2.0","id":10,"method":"eth_chainId","params":[]},
		{"jsonrpc":"2.0","id":20,"method":"eth_gasPrice","params":[]}
	]`
	req := httptest.NewRequest(http.MethodPost, "/", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	var batch []map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &batch); err != nil {
		t.Fatal(err)
	}
	if len(batch) != 3 {
		t.Fatalf("want 3 results, got %d", len(batch))
	}
	if batch[0]["error"] == nil {
		t.Fatal("slot 0 should be legacy gate error")
	}
	if batch[1]["result"] != "first" || batch[2]["result"] != "second" {
		t.Fatalf("unexpected merge order: %#v", batch)
	}
}

func TestWrapSeiLegacyHTTP_BatchMissingInnerResponseForID(t *testing.T) {
	inner := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(`[{"jsonrpc":"2.0","id":0,"result":"onlyOne"}]`))
	})
	h := wrapSeiLegacyHTTP(inner, BuildSeiLegacyEnabledSet(nil), 0)
	body := `[
		{"jsonrpc":"2.0","id":1,"method":"sei_getBlockByNumber","params":[]},
		{"jsonrpc":"2.0","id":10,"method":"eth_chainId","params":[]},
		{"jsonrpc":"2.0","id":99,"method":"eth_gasPrice","params":[]}
	]`
	req := httptest.NewRequest(http.MethodPost, "/", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	var batch []map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &batch); err != nil {
		t.Fatal(err)
	}
	if len(batch) != 3 {
		t.Fatalf("want 3 results, got %d", len(batch))
	}
	if batch[0]["error"] == nil {
		t.Fatal("slot 0 should be legacy gate error")
	}
	if batch[1]["result"] != "onlyOne" {
		t.Fatalf("slot 1: %+v", batch[1])
	}
	err2, _ := batch[2]["error"].(map[string]any)
	if err2 == nil {
		t.Fatal("slot 2 should be JSON-RPC error")
	}
	if int(err2["code"].(float64)) != internalErrorCode {
		t.Fatalf("want -32603, got %+v", err2)
	}
}

func TestWrapSeiLegacyHTTP_BatchInnerNotJSONArray(t *testing.T) {
	inner := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(`{"jsonrpc":"2.0","id":10,"result":"single"}`))
	})
	h := wrapSeiLegacyHTTP(inner, BuildSeiLegacyEnabledSet(nil), 0)
	body := `[
		{"jsonrpc":"2.0","id":1,"method":"sei_getBlockByNumber","params":[]},
		{"jsonrpc":"2.0","id":10,"method":"eth_chainId","params":[]},
		{"jsonrpc":"2.0","id":20,"method":"eth_gasPrice","params":[]}
	]`
	req := httptest.NewRequest(http.MethodPost, "/", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	var batch []map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &batch); err != nil {
		t.Fatal(err)
	}
	if len(batch) != 3 {
		t.Fatalf("want 3 results, got %d", len(batch))
	}
	if batch[0]["error"] == nil {
		t.Fatal("slot 0 should be legacy gate error")
	}
	for i := 1; i < len(batch); i++ {
		errObj, _ := batch[i]["error"].(map[string]any)
		if errObj == nil {
			t.Fatalf("slot %d expected error, got %+v", i, batch[i])
		}
		if int(errObj["code"].(float64)) != internalErrorCode {
			t.Fatalf("slot %d: %+v", i, errObj)
		}
	}
}

func TestMergeSeiLegacyHTTPBatch_PatchesMismatchedResponseID(t *testing.T) {
	blocked := []bool{false}
	var blockedErr []error
	ids := []json.RawMessage{json.RawMessage(`42`)}
	synthIDs := []json.RawMessage{json.RawMessage(`0`)}
	inner := []byte(`[{"jsonrpc":"2.0","id":0,"result":"x"}]`)
	invalid := []bool{false}
	out := mergeSeiLegacyHTTPBatch(invalid, blocked, blockedErr, ids, synthIDs, 1, inner)
	if len(out) != 1 {
		t.Fatalf("got %d", len(out))
	}
	var m map[string]json.RawMessage
	if err := json.Unmarshal(out[0], &m); err != nil {
		t.Fatal(err)
	}
	if string(m["id"]) != "42" {
		t.Fatalf("id not patched: %q", m["id"])
	}
}

func TestPatchJSONRPCResponseIDIfNeeded_PreservesLargeNumericID(t *testing.T) {
	// First integer not exactly representable as float64; must not round-trip through any/float64.
	const id = "9007199254740993"
	resp := json.RawMessage(`{"jsonrpc":"2.0","id":0,"result":"ok"}`)
	want := json.RawMessage(id)
	got := patchJSONRPCResponseIDIfNeeded(resp, want)
	var m map[string]json.RawMessage
	if err := json.Unmarshal(got, &m); err != nil {
		t.Fatal(err)
	}
	if string(m["id"]) != id {
		t.Fatalf("want id %s, got %q", id, m["id"])
	}
}

func TestPatchJSONRPCResponseIDIfNeeded_PreservesStringID(t *testing.T) {
	resp := json.RawMessage(`{"jsonrpc":"2.0","id":0,"result":"ok"}`)
	want := json.RawMessage(`"associate_addr"`)
	got := patchJSONRPCResponseIDIfNeeded(resp, want)
	var m map[string]json.RawMessage
	if err := json.Unmarshal(got, &m); err != nil {
		t.Fatal(err)
	}
	if string(m["id"]) != `"associate_addr"` {
		t.Fatalf("got %q", m["id"])
	}
}

func TestHasValidID(t *testing.T) {
	tests := []struct {
		name  string
		id    string
		valid bool
	}{
		{"integer", `1`, true},
		{"string", `"foo"`, true},
		{"null", `null`, true},
		{"empty (notification)", ``, false},
		{"object", `{}`, false},
		{"array", `[]`, false},
		{"object with fields", `{"k":"v"}`, false},
		{"array with items", `[1,2]`, false},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			m := &jsonrpcMessage{ID: json.RawMessage(tc.id)}
			if got := m.hasValidID(); got != tc.valid {
				t.Fatalf("hasValidID(%s) = %v, want %v", tc.id, got, tc.valid)
			}
		})
	}
}

// TestWrapSeiLegacyHTTP_BatchInvalidIDTypes verifies that batch elements with object or array IDs
// are rejected with -32600 and not forwarded. The valid slot in the batch must still be forwarded normally with
// its original ID restored in the response.
func TestWrapSeiLegacyHTTP_BatchInvalidIDTypes(t *testing.T) {
	inner := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		b, _ := io.ReadAll(r.Body)
		var fwd []map[string]any
		if err := json.Unmarshal(b, &fwd); err != nil || len(fwd) != 1 {
			t.Fatalf("inner should receive exactly one forwarded call, got err=%v body=%s", err, b)
		}
		if fwd[0]["method"] != "eth_chainId" {
			t.Fatalf("unexpected forwarded method: %v", fwd[0]["method"])
		}
		_, _ = w.Write([]byte(`[{"jsonrpc":"2.0","id":0,"result":"0x1"}]`))
	})
	h := wrapSeiLegacyHTTP(inner, BuildSeiLegacyEnabledSet(nil), 0)
	// slot 0: object id — invalid per hasValidID, must not be forwarded
	// slot 1: array id  — invalid per hasValidID, must not be forwarded
	// slot 2: integer id — valid, forwarded and original id restored in response
	body := `[
		{"jsonrpc":"2.0","id":{},"method":"eth_sendRawTransaction","params":["0x1337"]},
		{"jsonrpc":"2.0","id":[],"method":"eth_sendRawTransaction","params":["0x1337"]},
		{"jsonrpc":"2.0","id":1,"method":"eth_chainId","params":[]}
	]`
	req := httptest.NewRequest(http.MethodPost, "/", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	var batch []map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &batch); err != nil {
		t.Fatal(err)
	}
	if len(batch) != 3 {
		t.Fatalf("want 3 entries, got %d: %s", len(batch), rec.Body.String())
	}
	for _, slot := range []int{0, 1} {
		errObj, _ := batch[slot]["error"].(map[string]any)
		if errObj == nil || int(errObj["code"].(float64)) != invalidRequestCode {
			t.Fatalf("slot %d: want -32600, got %+v", slot, batch[slot])
		}
		if batch[slot]["id"] != nil {
			t.Fatalf("slot %d: want id null, got %v", slot, batch[slot]["id"])
		}
	}
	if batch[2]["result"] != "0x1" {
		t.Fatalf("slot 2: want result 0x1, got %+v", batch[2])
	}
	if id, _ := batch[2]["id"].(float64); int(id) != 1 {
		t.Fatalf("slot 2: want id 1, got %v", batch[2]["id"])
	}
}
