// Package rpc_io_test runs ethereum/execution-apis .io/.iox tests against a local Sei EVM RPC.
//
// Env: SEI_EVM_RPC_URL (default http://127.0.0.1:8545), SEI_IO_TESTS_DIR (default testdata/).
// Debug: SEI_EVM_IO_DEBUG_FILES="file1.iox,file2.io" to run only those files with extra logging.
package rpc_io_test

import (
	"encoding/json"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

var evmRPCSpecResults struct{ passed, failed, skipped int }

func TestNodeReachable(t *testing.T) {
	url := rpcURL()
	client := &RPCClient{URL: url}
	if !waitForNode(client, 2*time.Minute) {
		t.Fatalf("EVM RPC node not reachable at %s after 2 minutes", url)
	}
	t.Logf("RPC node reachable at %s", url)
}

// TestEVMRPCSpec runs each .io/.iox file as a subtest. Counts are stored for TestEVMRPCSpecSummary.
func TestEVMRPCSpec(t *testing.T) {
	abs, err := IOTestsDir()
	if err != nil {
		t.Fatalf("resolve io tests dir: %v", err)
	}
	if _, err := os.Stat(abs); os.IsNotExist(err) {
		t.Skipf("io tests dir not found: %s (copy execution-apis tests/ into testdata/)", abs)
	}
	files, err := CollectIOFiles(abs)
	if err != nil {
		t.Fatalf("collect .io files: %v", err)
	}
	if len(files) == 0 {
		t.Skipf("no .io files in %s", abs)
	}
	if list := os.Getenv("SEI_EVM_IO_DEBUG_FILES"); list != "" {
		allowed := make(map[string]bool)
		for _, s := range strings.Split(list, ",") {
			allowed[strings.TrimSpace(s)] = true
		}
		filtered := files[:0]
		for _, f := range files {
			if allowed[f] {
				filtered = append(filtered, f)
			}
		}
		if len(filtered) > 0 {
			files = filtered
		}
	}

	url := rpcURL()
	client := &RPCClient{URL: url}
	if !waitForNode(client, 60*time.Second) {
		t.Fatalf("EVM RPC node not reachable at %s after 60s", url)
	}

	debug := os.Getenv("SEI_EVM_IO_DEBUG_FILES") != ""
	seedBlock := os.Getenv("SEI_EVM_IO_SEED_BLOCK")
	var passed, failed, skipped int

	for _, rel := range files {
		rel := rel
		t.Run(rel, func(t *testing.T) {
			defer func() {
				switch {
				case t.Skipped():
					skipped++
				case t.Failed():
					failed++
				default:
					passed++
				}
			}()

			if debug {
				t.Logf("[DEBUG] SEI_EVM_IO_SEED_BLOCK=%q", seedBlock)
			}
			path := filepath.Join(abs, filepath.FromSlash(rel))
			content, err := os.ReadFile(path)
			if err != nil {
				t.Fatalf("read file: %v", err)
			}
			pairs, err := ParseIOFile(string(content))
			if err != nil {
				t.Fatalf("parse .io: %v", err)
			}
			if len(pairs) == 0 {
				t.Skip("no request/response pairs in file")
			}

			bindings := make(map[string]interface{})
			if deployTx := os.Getenv("SEI_EVM_IO_DEPLOY_TX_HASH"); deployTx != "" {
				bindings["deployTxHash"] = deployTx
				bindings["txHash"] = deployTx
			}
			responses := make([][]byte, len(pairs))

			for i, pair := range pairs {
				placeholders := requestPlaceholders(pair.Request)
				if debug {
					t.Logf("[DEBUG] pair %d: placeholders=%v bindings=%v", i+1, placeholders, bindings)
				}
				var missing []string
				for _, name := range placeholders {
					if _, ok := bindings[name]; !ok {
						missing = append(missing, name)
					}
				}
				if len(missing) > 0 {
					t.Logf("[SKIP] SEI_EVM_IO_SEED_BLOCK=%q SEI_EVM_IO_DEPLOY_TX_HASH set=%v", seedBlock, os.Getenv("SEI_EVM_IO_DEPLOY_TX_HASH") != "")
					t.Logf("[SKIP] pair %d needs %v; missing %v; bindings %v", i+1, placeholders, missing, bindings)
					t.Skipf("pair %d: missing binding ${%s}", i+1, missing[0])
				}

				req := SubstituteSeedTag(substituteRequest(pair.Request, bindings), seedBlock)
				if debug {
					t.Logf("[DEBUG] pair %d: request %s", i+1, req)
				}
				body, status, err := client.Call(req)
				if err != nil {
					t.Fatalf("pair %d: call: %v", i+1, err)
				}
				if status != http.StatusOK {
					t.Fatalf("pair %d: status %d body %s", i+1, status, body)
				}
				responses[i] = body
				if debug {
					logDebugResponse(t, body, i+1)
				}
				applyBindings(bindings, body, pair)
				if debug {
					t.Logf("[DEBUG] pair %d: bindings after apply: %v", i+1, bindings)
				}

				if pair.ExpectSameBlock != 0 {
					refIdx := pair.ExpectSameBlock - 1
					if refIdx < 0 || refIdx >= len(responses) || refIdx == i {
						t.Fatalf("pair %d: invalid @ expect_same_block %d", i+1, pair.ExpectSameBlock)
					}
					if !SameBlockResult(t, body, responses[refIdx]) {
						t.Fatalf("pair %d: expect_same_block %d check failed", i+1, pair.ExpectSameBlock)
					}
					continue
				}
				if len(pair.Expected) > 0 {
					if !SpecOnly(t, body, pair.Expected) {
						logActualResponse(t, body)
						t.Fatalf("pair %d: spec-only check failed", i+1)
					}
					continue
				}
				var m map[string]json.RawMessage
				if err := json.Unmarshal(body, &m); err != nil {
					t.Fatalf("pair %d: invalid JSON response", i+1)
				}
				if _, hasResult := m["result"]; !hasResult {
					if _, hasErr := m["error"]; !hasErr {
						t.Fatalf("pair %d: response has neither result nor error", i+1)
					}
				}
			}
		})
	}

	if passed+failed+skipped > 0 {
		evmRPCSpecResults.passed = passed
		evmRPCSpecResults.failed = failed
		evmRPCSpecResults.skipped = skipped
	}
}

// TestEVMRPCSpecSummary prints the report from the last TestEVMRPCSpec run (run after TestEVMRPCSpec).
func TestEVMRPCSpecSummary(t *testing.T) {
	p, f, s := evmRPCSpecResults.passed, evmRPCSpecResults.failed, evmRPCSpecResults.skipped
	total := p + f + s
	if total == 0 {
		return
	}
	rate := 0.0
	if p+f > 0 {
		rate = 100 * float64(p) / float64(p+f)
	}
	t.Logf("")
	t.Logf("========== Sei EVM RPC .io/.iox test report ==========")
	t.Logf("  Total:  %d", total)
	t.Logf("  Passed: %d", p)
	t.Logf("  Failed: %d", f)
	t.Logf("  Skipped: %d", s)
	t.Logf("  Pass rate: %.1f%%", rate)
	t.Logf("=======================================================")
}

func rpcURL() string {
	if u := os.Getenv("SEI_EVM_RPC_URL"); u != "" {
		return u
	}
	return "http://127.0.0.1:8545"
}

func nodeReachable(c *RPCClient) bool {
	body, status, err := c.Call([]byte(`{"jsonrpc":"2.0","id":1,"method":"eth_blockNumber","params":[]}`))
	if err != nil || status != http.StatusOK {
		return false
	}
	var m map[string]interface{}
	return json.Unmarshal(body, &m) == nil && (m["result"] != nil || m["error"] != nil)
}

// waitForNode retries nodeReachable until timeout. Returns true if the node became reachable.
func waitForNode(c *RPCClient, timeout time.Duration) bool {
	deadline := time.Now().Add(timeout)
	tick := 2 * time.Second
	for time.Now().Before(deadline) {
		if nodeReachable(c) {
			return true
		}
		time.Sleep(tick)
	}
	return false
}

func logActualResponse(t *testing.T, body []byte) {
	t.Helper()
	var m map[string]json.RawMessage
	if json.Unmarshal(body, &m) != nil {
		return
	}
	if e, ok := m["error"]; ok {
		var errObj struct {
			Code    int    `json:"code"`
			Message string `json:"message"`
		}
		_ = json.Unmarshal(e, &errObj)
		t.Logf("actual response: error code=%d message=%q (hint: -32601/not implemented, -32602/invalid params)", errObj.Code, errObj.Message)
		return
	}
	if r, ok := m["result"]; ok {
		s := string(r)
		if len(s) > 120 {
			s = s[:120] + "..."
		}
		t.Logf("actual response: result=%s", s)
	}
}

func logDebugResponse(t *testing.T, body []byte, pairIdx int) {
	t.Helper()
	var m map[string]json.RawMessage
	if json.Unmarshal(body, &m) != nil {
		return
	}
	if e, ok := m["error"]; ok {
		t.Logf("[DEBUG] pair %d: response error: %s", pairIdx, e)
		return
	}
	r, ok := m["result"]
	if !ok {
		return
	}
	var res interface{}
	if json.Unmarshal(r, &res) != nil {
		return
	}
	resM, ok := res.(map[string]interface{})
	if !ok {
		return
	}
	if txs, ok := resM["transactions"]; ok {
		n := -1
		if arr, ok := txs.([]interface{}); ok {
			n = len(arr)
		}
		t.Logf("[DEBUG] pair %d: result.transactions len=%d", pairIdx, n)
	}
}
