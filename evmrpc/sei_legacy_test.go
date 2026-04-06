package evmrpc

import (
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/ethereum/go-ethereum/rpc"
)

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
	h := wrapSeiLegacyHTTP(inner, enabled)
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
	h := wrapSeiLegacyHTTP(inner, BuildSeiLegacyEnabledSet(nil))
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

func TestWrapSeiLegacyHTTP_AllowedMethodPassthroughAndDeprecationHeader(t *testing.T) {
	called := false
	inner := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		called = true
		_, _ = w.Write([]byte(`{"jsonrpc":"2.0","id":1,"result":{"number":"0x1"}}`))
	})
	enabled := BuildSeiLegacyEnabledSet([]string{"sei_getBlockByNumber"})
	h := wrapSeiLegacyHTTP(inner, enabled)
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
	h := wrapSeiLegacyHTTP(inner, enabled)
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
	h := wrapSeiLegacyHTTP(inner, BuildSeiLegacyEnabledSet(nil))
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
	h := wrapSeiLegacyHTTP(inner, enabled)
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
	h := wrapSeiLegacyHTTP(inner, BuildSeiLegacyEnabledSet(nil))
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
	h := wrapSeiLegacyHTTP(inner, BuildSeiLegacyEnabledSet(nil))
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
	if err1 == nil || int(err1["code"].(float64)) != -32600 {
		t.Fatalf("slot 1 should be JSON-RPC invalid request (-32600): %+v", batch[1])
	}
}

func TestWrapSeiLegacyHTTP_BatchLeadingNonObjectDoesNotBypassGate(t *testing.T) {
	inner := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		t.Fatal("inner must not run when all batch slots are answered by the gate")
	})
	h := wrapSeiLegacyHTTP(inner, BuildSeiLegacyEnabledSet(nil))
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
	if err0 == nil || int(err0["code"].(float64)) != -32600 {
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
		_, _ = w.Write([]byte(`[{"jsonrpc":"2.0","id":2,"result":"0x1"}]`))
	})
	h := wrapSeiLegacyHTTP(inner, BuildSeiLegacyEnabledSet(nil))
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
		_, _ = w.Write([]byte(`[{"jsonrpc":"2.0","id":1,"result":"0xaa"}]`))
	})
	h := wrapSeiLegacyHTTP(inner, BuildSeiLegacyEnabledSet(nil))
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
	if err1 == nil || int(err1["code"].(float64)) != -32600 {
		t.Fatalf("slot 1 want -32600: %+v", batch[1])
	}
}

func TestWrapSeiLegacyHTTP_BatchInnerReorderedByID(t *testing.T) {
	inner := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		// Inner returns results permuted vs forwarded request order.
		_, _ = w.Write([]byte(`[
			{"jsonrpc":"2.0","id":20,"result":"second"},
			{"jsonrpc":"2.0","id":10,"result":"first"}
		]`))
	})
	h := wrapSeiLegacyHTTP(inner, BuildSeiLegacyEnabledSet(nil))
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
		_, _ = w.Write([]byte(`[{"jsonrpc":"2.0","id":10,"result":"onlyOne"}]`))
	})
	h := wrapSeiLegacyHTTP(inner, BuildSeiLegacyEnabledSet(nil))
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
	if int(err2["code"].(float64)) != -32603 {
		t.Fatalf("want -32603, got %+v", err2)
	}
}

func TestWrapSeiLegacyHTTP_BatchInnerNotJSONArray(t *testing.T) {
	inner := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(`{"jsonrpc":"2.0","id":10,"result":"single"}`))
	})
	h := wrapSeiLegacyHTTP(inner, BuildSeiLegacyEnabledSet(nil))
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
		if int(errObj["code"].(float64)) != -32603 {
			t.Fatalf("slot %d: %+v", i, errObj)
		}
	}
}

func TestMergeSeiLegacyHTTPBatch_PatchesMismatchedResponseID(t *testing.T) {
	blocked := []bool{false}
	var blockedErr []error
	ids := []json.RawMessage{json.RawMessage(`42`)}
	inner := []byte(`[{"jsonrpc":"2.0","id":0,"result":"x"}]`)
	invalid := []bool{false}
	out := mergeSeiLegacyHTTPBatch(invalid, blocked, blockedErr, ids, 1, inner)
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
