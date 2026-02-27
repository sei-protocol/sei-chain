package rpc_io_test

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

func TestParseIOFile_BindAndRefPair(t *testing.T) {
	content := `>> {"method":"eth_getBlockByNumber","params":["latest",false]}
<< {"jsonrpc":"2.0","id":1,"result":{}}
@ bind blockHash = result.hash
>> {"method":"eth_getBlockByHash","params":["${blockHash}",false]}
<< @ ref_pair 1
`
	pairs, err := parseIOFile(content)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	if len(pairs) != 2 {
		t.Fatalf("expected 2 pairs, got %d", len(pairs))
	}
	if len(pairs[0].AfterBindings) != 1 || pairs[0].AfterBindings[0].Var != "blockHash" || pairs[0].AfterBindings[0].Path != "result.hash" {
		t.Fatalf("pair 0 AfterBindings: got %+v", pairs[0].AfterBindings)
	}
	if pairs[1].RefPair != 1 {
		t.Fatalf("pair 1 RefPair: got %d", pairs[1].RefPair)
	}
	if !bytes.Contains(pairs[1].Request, []byte("${blockHash}")) {
		t.Fatalf("pair 1 request should contain ${blockHash}")
	}
}

func TestParseIOFile_PlainIO(t *testing.T) {
	content := `>> {"method":"eth_chainId","params":[]}
<< {"jsonrpc":"2.0","id":1,"result":"0x1"}
`
	pairs, err := parseIOFile(content)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	if len(pairs) != 1 {
		t.Fatalf("expected 1 pair, got %d", len(pairs))
	}
	if len(pairs[0].AfterBindings) != 0 {
		t.Fatalf("plain .io should have no AfterBindings: %+v", pairs[0].AfterBindings)
	}
	if pairs[0].RefPair != 0 {
		t.Fatalf("plain .io RefPair should be 0: %d", pairs[0].RefPair)
	}
	if len(pairs[0].Expected) == 0 {
		t.Fatal("expected non-empty Expected")
	}
}

func TestParseIOFile_MultipleBindings(t *testing.T) {
	content := `>> {"method":"eth_getBlockByNumber","params":["latest",false]}
<< {"jsonrpc":"2.0","id":1,"result":{"hash":"0xaa","number":"0x2d"}}
@ bind blockHash = result.hash
@ bind blockNum = result.number
>> {"method":"eth_getBlockByNumber","params":["${blockNum}",false]}
<< @ ref_pair 1
`
	pairs, err := parseIOFile(content)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	if len(pairs) != 2 {
		t.Fatalf("expected 2 pairs, got %d", len(pairs))
	}
	if len(pairs[0].AfterBindings) != 2 {
		t.Fatalf("expected 2 bindings, got %d", len(pairs[0].AfterBindings))
	}
	vars := map[string]string{}
	for _, b := range pairs[0].AfterBindings {
		vars[b.Var] = b.Path
	}
	if vars["blockHash"] != "result.hash" || vars["blockNum"] != "result.number" {
		t.Fatalf("bindings: %+v", vars)
	}
}

func TestParseIOFile_CommentsAndWhitespace(t *testing.T) {
	content := `  // comment
>> {"method":"eth_chainId"}
<< {"result":"0x1"}
`
	pairs, err := parseIOFile(content)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	if len(pairs) != 1 {
		t.Fatalf("expected 1 pair (comment ignored), got %d", len(pairs))
	}
}

func TestParseIOFile_RefPairOnly(t *testing.T) {
	content := `>> {"method":"eth_getBlockByHash","params":["0xabc",false]}
<< @ ref_pair 1
`
	pairs, err := parseIOFile(content)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	if len(pairs) != 1 {
		t.Fatalf("expected 1 pair, got %d", len(pairs))
	}
	if pairs[0].RefPair != 1 {
		t.Fatalf("RefPair: got %d", pairs[0].RefPair)
	}
	if len(pairs[0].Expected) != 0 {
		t.Fatalf("expected empty Expected when using directive, got %d bytes", len(pairs[0].Expected))
	}
}

func TestGetJSONPath(t *testing.T) {
	body := []byte(`{"jsonrpc":"2.0","id":1,"result":{"hash":"0xabc","number":"0x2d"}}`)
	if v, ok := getJSONPath(body, "result.hash"); !ok || v != "0xabc" {
		t.Errorf("result.hash: got %v, %v", v, ok)
	}
	if v, ok := getJSONPath(body, "result.number"); !ok || v != "0x2d" {
		t.Errorf("result.number: got %v, %v", v, ok)
	}
	if _, ok := getJSONPath(body, "result.missing"); ok {
		t.Error("result.missing should not be found")
	}
}

func TestGetJSONPath_Nested(t *testing.T) {
	body := []byte(`{"a":{"b":{"c":"v"}}}`)
	if v, ok := getJSONPath(body, "a.b.c"); !ok || v != "v" {
		t.Errorf("a.b.c: got %v, %v", v, ok)
	}
	if _, ok := getJSONPath(body, "a.b.x"); ok {
		t.Error("a.b.x should not be found")
	}
}

func TestGetJSONPath_InvalidJSON(t *testing.T) {
	if _, ok := getJSONPath([]byte("not json"), "result.hash"); ok {
		t.Error("invalid JSON should not be found")
	}
}

func TestGetJSONPath_NumberInResponse(t *testing.T) {
	body := []byte(`{"result":{"count":42}}`)
	if v, ok := getJSONPath(body, "result.count"); !ok {
		t.Errorf("result.count not found")
	} else if f, ok := v.(float64); !ok || f != 42 {
		t.Errorf("result.count: got %v (%T)", v, v)
	}
}

func TestGetJSONPath_ArrayIndex(t *testing.T) {
	body := []byte(`{"result":{"transactions":[{"hash":"0xtx1"},{"hash":"0xtx2"}]}}`)
	if v, ok := getJSONPath(body, "result.transactions.0.hash"); !ok || v != "0xtx1" {
		t.Errorf("result.transactions.0.hash: got %v, %v", v, ok)
	}
	if v, ok := getJSONPath(body, "result.transactions.1.hash"); !ok || v != "0xtx2" {
		t.Errorf("result.transactions.1.hash: got %v, %v", v, ok)
	}
	if _, ok := getJSONPath(body, "result.transactions.2.hash"); ok {
		t.Error("out of range index should not be found")
	}
	if _, ok := getJSONPath(body, "result.transactions.x.hash"); ok {
		t.Error("non-numeric index should not be found")
	}
}

func TestSubstituteRequest(t *testing.T) {
	bindings := map[string]any{"blockHash": "0xabc123"}
	req := []byte(`{"params":["${blockHash}",false]}`)
	out := substituteRequest(req, bindings)
	if !bytes.Contains(out, []byte("0xabc123")) {
		t.Errorf("substituteRequest: got %s", out)
	}
	if bytes.Contains(out, []byte("${blockHash}")) {
		t.Errorf("placeholder should be replaced")
	}
}

func TestSubstituteRequest_EmptyBindings(t *testing.T) {
	req := []byte(`{"params":["${blockHash}"]}`)
	out := substituteRequest(req, nil)
	if !bytes.Equal(out, req) {
		t.Errorf("nil bindings should return unchanged: got %s", out)
	}
	out = substituteRequest(req, map[string]any{})
	if !bytes.Equal(out, req) {
		t.Errorf("empty bindings should return unchanged: got %s", out)
	}
}

func TestSubstituteRequest_MultipleVars(t *testing.T) {
	bindings := map[string]any{"hash": "0xaa", "num": "0x2d"}
	req := []byte(`{"hash":"${hash}","number":"${num}"}`)
	out := substituteRequest(req, bindings)
	if !bytes.Contains(out, []byte("0xaa")) || !bytes.Contains(out, []byte("0x2d")) {
		t.Errorf("substituteRequest multiple: got %s", out)
	}
	if bytes.Contains(out, []byte("${")) {
		t.Errorf("no placeholder should remain")
	}
}

func TestSubstituteRequest_NoPlaceholder(t *testing.T) {
	req := []byte(`{"params":["latest"]}`)
	bindings := map[string]any{"blockHash": "0xabc"}
	out := substituteRequest(req, bindings)
	if !bytes.Equal(out, req) {
		t.Errorf("request without placeholder should be unchanged: got %s", out)
	}
}

func TestSubstituteRequest_NumberBinding(t *testing.T) {
	bindings := map[string]any{"n": float64(42)}
	req := []byte(`{"blockNumber":"${n}"}`)
	out := substituteRequest(req, bindings)
	if !bytes.Contains(out, []byte("42")) {
		t.Errorf("number binding: got %s", out)
	}
}

func TestSubstituteSeedTag(t *testing.T) {
	req := []byte(`{"jsonrpc":"2.0","id":1,"method":"eth_getBlockByNumber","params":["__SEED__",true]}`)
	out := substituteSeedTag(req, "0x2a")
	var m map[string]any
	if err := json.Unmarshal(out, &m); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	params, _ := m["params"].([]any)
	if len(params) < 1 || params[0] != "0x2a" {
		t.Errorf("expected params[0]=0x2a, got %v", params)
	}
	out2 := substituteSeedTag(req, "")
	if err := json.Unmarshal(out2, &m); err != nil {
		t.Fatalf("unmarshal empty: %v", err)
	}
	params, _ = m["params"].([]any)
	if len(params) < 1 || params[0] != "latest" {
		t.Errorf("expected params[0]=latest when seed empty, got %v", params)
	}
}

func TestRequestPlaceholders(t *testing.T) {
	req := []byte(`{"params":["${blockHash}",false]}`)
	got := requestPlaceholders(req)
	if len(got) != 1 || got[0] != "blockHash" {
		t.Errorf("requestPlaceholders: got %v", got)
	}
	req2 := []byte(`{"a":"${x}","b":"${y}"}`)
	got2 := requestPlaceholders(req2)
	if len(got2) != 2 {
		t.Errorf("requestPlaceholders two: got %v", got2)
	}
	req3 := []byte(`{"params":["latest"]}`)
	got3 := requestPlaceholders(req3)
	if len(got3) != 0 {
		t.Errorf("requestPlaceholders none: got %v", got3)
	}
}

func TestApplyBindings(t *testing.T) {
	response := []byte(`{"jsonrpc":"2.0","id":1,"result":{"hash":"0xhh","number":"0x2d"}}`)
	pair := ioxPair{
		AfterBindings: []binding{
			{Var: "blockHash", Path: "result.hash"},
			{Var: "blockNumber", Path: "result.number"},
		},
	}
	bindings := make(map[string]any)
	applyBindings(bindings, response, pair)
	if bindings["blockHash"] != "0xhh" || bindings["blockNumber"] != "0x2d" {
		t.Errorf("applyBindings: got %+v", bindings)
	}
}

func TestApplyBindings_MissingPath(t *testing.T) {
	response := []byte(`{"result":{"hash":"0xhh"}}`)
	pair := ioxPair{
		AfterBindings: []binding{
			{Var: "blockHash", Path: "result.hash"},
			{Var: "missing", Path: "result.missing"},
		},
	}
	bindings := make(map[string]any)
	applyBindings(bindings, response, pair)
	if bindings["blockHash"] != "0xhh" {
		t.Errorf("blockHash should be set: %+v", bindings)
	}
	if _, ok := bindings["missing"]; ok {
		t.Error("missing path should not be set")
	}
}

func TestApplyBindings_EmptyBindings(t *testing.T) {
	bindings := make(map[string]any)
	applyBindings(bindings, []byte(`{}`), ioxPair{})
	if len(bindings) != 0 {
		t.Errorf("empty AfterBindings should not change map: %+v", bindings)
	}
}

func TestSameBlockResult(t *testing.T) {
	ref := []byte(`{"jsonrpc":"2.0","id":1,"result":{"number":"0x2d","hash":"0xabc"}}`)
	same := []byte(`{"jsonrpc":"2.0","id":1,"result":{"number":"0x2d","hash":"0xabc"}}`)
	diff := []byte(`{"jsonrpc":"2.0","id":1,"result":{"number":"0x2e","hash":"0xdef"}}`)
	if !sameBlockResult(t, same, ref) {
		t.Error("same block should pass")
	}
	if sameBlockResult(t, diff, ref) {
		t.Error("different block should fail")
	}
}

func TestSameBlockResult_MissingResult(t *testing.T) {
	ref := []byte(`{"jsonrpc":"2.0","id":1,"result":{"number":"0x2d","hash":"0xabc"}}`)
	noResult := []byte(`{"jsonrpc":"2.0","id":1,"error":{"code":-32602,"message":"missing"}}`)
	if sameBlockResult(t, noResult, ref) {
		t.Error("actual with error should fail")
	}
	noHash := []byte(`{"jsonrpc":"2.0","id":1,"result":{"number":"0x2d"}}`)
	if sameBlockResult(t, noHash, ref) {
		t.Error("actual missing hash should fail")
	}
}

func TestSpecOnly(t *testing.T) {
	expected := []byte(`{"jsonrpc":"2.0","id":1,"result":"0x1"}`)
	actualResult := []byte(`{"jsonrpc":"2.0","id":1,"result":"0x2"}`)
	actualError := []byte(`{"jsonrpc":"2.0","id":1,"error":{"code":-32600,"message":"err"}}`)
	if !specOnly(t, actualResult, expected) {
		t.Error("SpecOnly: result vs result should pass")
	}
	expErr := []byte(`{"jsonrpc":"2.0","id":1,"error":{}}`)
	if !specOnly(t, actualError, expErr) {
		t.Error("SpecOnly: error vs error should pass")
	}
	if specOnly(t, actualError, expected) {
		t.Error("SpecOnly: error vs result should fail")
	}
	if specOnly(t, actualResult, expErr) {
		t.Error("SpecOnly: result vs error should fail")
	}
}

func TestSpecOnly_InvalidJSON(t *testing.T) {
	if specOnly(t, []byte("not json"), []byte(`{"result":1}`)) {
		t.Error("invalid actual JSON should fail")
	}
	if specOnly(t, []byte(`{"result":1}`), []byte("not json")) {
		t.Error("invalid expected JSON should fail")
	}
}

func TestRunnerFlow_SubstitutionAndSameBlock(t *testing.T) {
	pairs := []ioxPair{
		{
			Request:       []byte(`{"method":"eth_getBlockByNumber","params":["latest",false]}`),
			Expected:      []byte(`{"jsonrpc":"2.0","id":1,"result":{}}`),
			AfterBindings: []binding{{Var: "blockHash", Path: "result.hash"}},
		},
		{
			Request: []byte(`{"method":"eth_getBlockByHash","params":["${blockHash}",false]}`),
			RefPair: 1,
		},
	}
	resp1 := []byte(`{"jsonrpc":"2.0","id":1,"result":{"number":"0x2d","hash":"0xabc"}}`)
	resp2 := []byte(`{"jsonrpc":"2.0","id":1,"result":{"number":"0x2d","hash":"0xabc"}}`)

	bindings := make(map[string]any)
	responses := make([][]byte, 2)

	req0 := substituteRequest(pairs[0].Request, bindings)
	if len(req0) == 0 {
		t.Fatal("request 0 empty")
	}
	responses[0] = resp1
	applyBindings(bindings, resp1, pairs[0])
	if bindings["blockHash"] != "0xabc" {
		t.Fatalf("binding not set: %+v", bindings)
	}

	req1 := substituteRequest(pairs[1].Request, bindings)
	if bytes.Contains(req1, []byte("${blockHash}")) {
		t.Fatalf("placeholder not substituted: %s", req1)
	}
	if !bytes.Contains(req1, []byte("0xabc")) {
		t.Fatalf("substituted value missing: %s", req1)
	}
	responses[1] = resp2
	if !sameBlockResult(t, resp2, responses[0]) {
		t.Fatal("ref_pair check should pass")
	}
}

func TestCollectIOFiles_IncludesIOAndIox(t *testing.T) {
	dir := t.TempDir()
	for _, name := range []string{"a.io", "b.iox", "c.io", "skip.txt"} {
		if err := os.WriteFile(filepath.Join(dir, name), []byte(">> {}\n<< {}"), 0644); err != nil {
			t.Fatalf("write %s: %v", name, err)
		}
	}
	files, err := collectIOFiles(dir)
	if err != nil {
		t.Fatalf("collectIOFiles: %v", err)
	}
	if len(files) != 3 {
		t.Fatalf("expected 3 files, got %d: %v", len(files), files)
	}
	got := make(map[string]bool)
	for _, f := range files {
		got[filepath.Base(f)] = true
	}
	if !got["a.io"] || !got["b.iox"] || !got["c.io"] {
		t.Error("expected a.io, b.iox, c.io to be collected")
	}
}

func TestIOTestsDir(t *testing.T) {
	dir, err := ioTestsDir()
	if err != nil {
		t.Fatalf("ioTestsDir: %v", err)
	}
	if dir == "" {
		t.Fatal("ioTestsDir returned empty")
	}
	if _, err := os.Stat(dir); err != nil {
		t.Fatalf("ioTestsDir path not exist: %v", err)
	}
}
