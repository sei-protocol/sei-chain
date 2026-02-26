package rpc_io_test

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"runtime"
	"sort"
	"strings"
	"sync"
	"testing"
	"time"
)

const (
	reqPrefix       = ">> "
	respPrefix      = "<< "
	directivePrefix = "@ "
)

// Binding maps a variable name to a JSON path in the response; the value is available as ${varName} in later requests.
type Binding struct {
	Var  string
	Path string
}

// IOPair is one request/response pair. ExpectSameBlock is 1-based pair index for same-block cross-check.
type IOPair struct {
	Request         []byte
	Expected        []byte
	AfterBindings   []Binding
	ExpectSameBlock int
}

// ParseIOFile parses .io content into request/response pairs. Supports ">> request", "<< expected",
// "@ bind varName = path", and "<< @ expect_same_block N".
func ParseIOFile(content string) ([]IOPair, error) {
	var pairs []IOPair
	var curReq []byte
	lastIdx := -1
	inBinding := false

	for _, line := range strings.Split(content, "\n") {
		trimmed := strings.TrimSpace(strings.TrimSuffix(line, "\r"))
		if trimmed == "" {
			continue
		}

		if strings.HasPrefix(trimmed, reqPrefix) {
			curReq = []byte(strings.TrimPrefix(trimmed, reqPrefix))
			inBinding = false
			continue
		}

		if strings.HasPrefix(trimmed, respPrefix) {
			rest := strings.TrimSpace(strings.TrimPrefix(trimmed, respPrefix))
			if len(curReq) == 0 {
				continue
			}
			if strings.HasPrefix(rest, directivePrefix) {
				var n int
				if _, err := fmt.Sscanf(strings.TrimPrefix(rest, directivePrefix), "expect_same_block %d", &n); err == nil && n >= 1 {
					pairs = append(pairs, IOPair{Request: curReq, ExpectSameBlock: n})
					lastIdx = len(pairs) - 1
					inBinding = true
				}
			} else {
				pairs = append(pairs, IOPair{Request: curReq, Expected: []byte(strings.TrimPrefix(trimmed, respPrefix))})
				lastIdx = len(pairs) - 1
				inBinding = true
			}
			curReq = nil
			continue
		}

		if inBinding && lastIdx >= 0 && strings.HasPrefix(trimmed, directivePrefix) {
			rest := strings.TrimSpace(strings.TrimPrefix(trimmed, directivePrefix))
			if after, ok := strings.CutPrefix(rest, "bind "); ok {
				if idx := strings.Index(after, "="); idx > 0 {
					varName := strings.TrimSpace(after[:idx])
					path := strings.TrimSpace(after[idx+1:])
					if varName != "" && path != "" {
						pairs[lastIdx].AfterBindings = append(pairs[lastIdx].AfterBindings, Binding{Var: varName, Path: path})
					}
				}
			}
		}
	}
	return pairs, nil
}

const rpcCallTimeout = 30 * time.Second

// RPCClient sends JSON-RPC requests. If Client is nil, a default client is used and reused.
type RPCClient struct {
	URL    string
	Client *http.Client

	once   sync.Once
	client *http.Client
}

func (c *RPCClient) httpClient() *http.Client {
	if c.Client != nil {
		return c.Client
	}
	c.once.Do(func() {
		c.client = &http.Client{Timeout: rpcCallTimeout}
	})
	return c.client
}

func (c *RPCClient) Call(req []byte) ([]byte, int, error) {
	resp, err := c.httpClient().Post(c.URL, "application/json", bytes.NewReader(req))
	if err != nil {
		return nil, 0, err
	}
	defer resp.Body.Close()
	var buf bytes.Buffer
	if _, err := buf.ReadFrom(resp.Body); err != nil {
		return nil, resp.StatusCode, err
	}
	return buf.Bytes(), resp.StatusCode, nil
}

// getJSONPath resolves a dot path (e.g. "result.hash", "result.transactions.0") in body. Supports object keys and array indices.
func getJSONPath(body []byte, path string) (interface{}, bool) {
	var root interface{}
	if err := json.Unmarshal(body, &root); err != nil {
		return nil, false
	}
	current := root
	for _, p := range strings.Split(path, ".") {
		if p == "" {
			continue
		}
		if m, ok := current.(map[string]interface{}); ok {
			var next interface{}
			next, ok = m[p]
			if !ok {
				return nil, false
			}
			current = next
			continue
		}
		if arr, ok := current.([]interface{}); ok {
			var idx int
			if _, err := fmt.Sscanf(p, "%d", &idx); err != nil || idx < 0 || idx >= len(arr) {
				return nil, false
			}
			current = arr[idx]
			continue
		}
		return nil, false
	}
	return current, true
}

func requestPlaceholders(request []byte) []string {
	var out []string
	s := string(request)
	for {
		start := strings.Index(s, "${")
		if start < 0 {
			break
		}
		end := strings.Index(s[start+2:], "}")
		if end < 0 {
			break
		}
		out = append(out, s[start+2:start+2+end])
		s = s[start+2+end+1:]
	}
	return out
}

// SubstituteSeedTag replaces quoted "__SEED__" in request with seedBlock, or "latest" if seedBlock is empty.
func SubstituteSeedTag(request []byte, seedBlock string) []byte {
	v := "latest"
	if seedBlock != "" {
		v = seedBlock
	}
	return []byte(strings.ReplaceAll(string(request), `"__SEED__"`, `"`+v+`"`))
}

func substituteRequest(request []byte, bindings map[string]interface{}) []byte {
	if len(bindings) == 0 {
		return request
	}
	s := string(request)
	for name, val := range bindings {
		placeholder := "${" + name + "}"
		if !strings.Contains(s, placeholder) {
			continue
		}
		var repl string
		switch v := val.(type) {
		case string:
			repl = v
		case float64:
			repl = fmt.Sprintf("%.0f", v)
		case nil:
			repl = "null"
		default:
			repl = fmt.Sprint(v)
		}
		s = strings.ReplaceAll(s, placeholder, repl)
	}
	return []byte(s)
}

func applyBindings(bindings map[string]interface{}, response []byte, pair IOPair) {
	for _, b := range pair.AfterBindings {
		if v, ok := getJSONPath(response, b.Path); ok {
			bindings[b.Var] = v
		}
	}
}

// SameBlockResult reports whether actual and reference are both block results with the same number and hash.
func SameBlockResult(t *testing.T, actual, reference []byte) bool {
	t.Helper()
	numA, okNumA := getJSONPath(actual, "result.number")
	hashA, okHashA := getJSONPath(actual, "result.hash")
	numR, okNumR := getJSONPath(reference, "result.number")
	hashR, okHashR := getJSONPath(reference, "result.hash")
	if !okNumA || !okHashA || !okNumR || !okHashR {
		t.Log("missing result.number or result.hash in actual or reference")
		return false
	}
	if numA != numR || hashA != hashR {
		t.Logf("block mismatch: actual number=%v hash=%v, reference number=%v hash=%v", numA, hashA, numR, hashR)
		return false
	}
	return true
}

// SpecOnly checks that actual has the same result/error shape as expected (no value comparison).
func SpecOnly(t *testing.T, actual, expected []byte) bool {
	t.Helper()
	var exp, act map[string]json.RawMessage
	if err := json.Unmarshal(expected, &exp); err != nil {
		t.Logf("invalid expected JSON: %v", err)
		return false
	}
	if err := json.Unmarshal(actual, &act); err != nil {
		t.Logf("invalid actual JSON: %v", err)
		return false
	}
	has := func(m map[string]json.RawMessage, k string) bool { _, ok := m[k]; return ok }
	if has(exp, "result") != has(act, "result") || has(exp, "error") != has(act, "error") {
		t.Logf("response kind mismatch: expected result=%v error=%v, actual result=%v error=%v",
			has(exp, "result"), has(exp, "error"), has(act, "result"), has(act, "error"))
		return false
	}
	if _, ok := act["jsonrpc"]; !ok {
		t.Log("actual missing jsonrpc field")
		return false
	}
	return true
}

// IOTestsDir returns the testdata path. Uses SEI_IO_TESTS_DIR if set.
func IOTestsDir() (string, error) {
	if d := os.Getenv("SEI_IO_TESTS_DIR"); d != "" {
		return filepath.Abs(d)
	}
	_, filename, _, ok := runtime.Caller(0)
	if !ok {
		return "", os.ErrNotExist
	}
	return filepath.Join(filepath.Dir(filename), "testdata"), nil
}

// CollectIOFiles returns relative paths of all .io and .iox files under dir, sorted.
func CollectIOFiles(dir string) ([]string, error) {
	var out []string
	err := filepath.Walk(dir, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		if info.IsDir() {
			return nil
		}
		ext := filepath.Ext(path)
		if ext != ".io" && ext != ".iox" {
			return nil
		}
		rel, err := filepath.Rel(dir, path)
		if err != nil {
			return err
		}
		out = append(out, filepath.ToSlash(rel))
		return nil
	})
	if err != nil {
		return nil, err
	}
	sort.Strings(out)
	return out, nil
}
