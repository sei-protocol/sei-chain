package rpc_io_test

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
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

const rpcCallTimeout = 30 * time.Second

type rpcClient struct {
	URL    string
	Client *http.Client

	once   sync.Once
	client *http.Client
}

type binding struct {
	Var  string
	Path string
}

// ioxPair is one request/response pair from a .io/.iox file.
type ioxPair struct {
	Request       []byte
	Expected      []byte
	AfterBindings []binding
	RefPair       int // 1-based; 0 = no ref check
}

// parseIOFile parses .io/.iox content. Supports ">> request", "<< expected", "@ bind var = path", "<< @ ref_pair N".
func parseIOFile(content string) ([]ioxPair, error) {
	var pairs []ioxPair
	var curReq []byte
	lastIdx := -1
	inBinding := false

	for line := range strings.SplitSeq(content, "\n") {
		trimmed := strings.TrimSpace(strings.TrimSuffix(line, "\r"))
		if trimmed == "" {
			continue
		}

		if after, ok := strings.CutPrefix(trimmed, reqPrefix); ok {
			curReq = []byte(strings.TrimSpace(after))
			inBinding = false
			continue
		}

		if after, ok := strings.CutPrefix(trimmed, respPrefix); ok {
			rest := strings.TrimSpace(after)
			if len(curReq) == 0 {
				continue
			}
			if after, ok := strings.CutPrefix(rest, directivePrefix); ok {
				rest = strings.TrimSpace(after)
				var n int
				if _, err := fmt.Sscanf(rest, "ref_pair %d", &n); err == nil && n >= 1 {
					pairs = append(pairs, ioxPair{Request: curReq, RefPair: n})
					lastIdx = len(pairs) - 1
					inBinding = true
				}
				curReq = nil
				continue
			}

			pairs = append(pairs, ioxPair{Request: curReq, Expected: []byte(strings.TrimPrefix(trimmed, respPrefix))})
			lastIdx = len(pairs) - 1
			inBinding = true
			curReq = nil
			continue
		}

		if inBinding && lastIdx >= 0 && strings.HasPrefix(trimmed, directivePrefix) {
			rest := strings.TrimSpace(strings.TrimPrefix(trimmed, directivePrefix))
			if after, ok := strings.CutPrefix(rest, "bind "); ok && strings.Contains(after, "=") {
				idx := strings.Index(after, "=")
				varName := strings.TrimSpace(after[:idx])
				path := strings.TrimSpace(after[idx+1:])
				if varName != "" && path != "" {
					pairs[lastIdx].AfterBindings = append(pairs[lastIdx].AfterBindings, binding{Var: varName, Path: path})
				}
			}
		}
	}
	return pairs, nil
}

func (c *rpcClient) call(req []byte) ([]byte, int, error) {
	resp, err := c.httpClient().Post(c.URL, "application/json", bytes.NewReader(req))
	if err != nil {
		return nil, 0, err
	}
	defer func() { _ = resp.Body.Close() }()
	var buf bytes.Buffer
	if _, err := buf.ReadFrom(resp.Body); err != nil {
		return nil, resp.StatusCode, err
	}
	return buf.Bytes(), resp.StatusCode, nil
}

func (c *rpcClient) httpClient() *http.Client {
	if c.Client != nil {
		return c.Client
	}
	c.once.Do(func() {
		c.client = &http.Client{Timeout: rpcCallTimeout}
	})
	return c.client
}

func getJSONPath(body []byte, path string) (any, bool) {
	var root any
	if err := json.Unmarshal(body, &root); err != nil {
		return nil, false
	}
	current := root
	for p := range strings.SplitSeq(path, ".") {
		if p == "" {
			continue
		}
		if m, ok := current.(map[string]any); ok {
			next, ok := m[p]
			if !ok {
				return nil, false
			}
			current = next
			continue
		}
		if arr, ok := current.([]any); ok {
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

func substituteSeedTag(request []byte, seedBlock string) []byte {
	v := "latest"
	if seedBlock != "" {
		v = seedBlock
	}
	return []byte(strings.ReplaceAll(string(request), `"__SEED__"`, `"`+v+`"`))
}

func substituteRequest(request []byte, bindings map[string]any) []byte {
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

func applyBindings(bindings map[string]any, response []byte, pair ioxPair) {
	for _, b := range pair.AfterBindings {
		if v, ok := getJSONPath(response, b.Path); ok {
			bindings[b.Var] = v
		}
	}
}

func sameBlockResult(t *testing.T, actual, reference []byte) bool {
	t.Helper()
	numA, okA := getJSONPath(actual, "result.number")
	hashA, okB := getJSONPath(actual, "result.hash")
	numR, okR := getJSONPath(reference, "result.number")
	hashR, okS := getJSONPath(reference, "result.hash")
	if !okA || !okB || !okR || !okS {
		t.Log("missing result.number or result.hash in actual or reference")
		return false
	}
	if numA != numR || hashA != hashR {
		t.Logf("block mismatch: actual number=%v hash=%v, reference number=%v hash=%v", numA, hashA, numR, hashR)
		return false
	}
	return true
}

func specOnly(t *testing.T, actual, expected []byte) bool {
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

func ioTestsDir() (string, error) {
	cwd, err := os.Getwd()
	if err != nil {
		return "", err
	}
	return filepath.Join(cwd, "testdata"), nil
}

func collectIOFiles(dir string) ([]string, error) {
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
