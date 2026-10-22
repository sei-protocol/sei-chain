package parity

import (
	"bytes"
	"encoding/json"
	"fmt"
	"maps"
	"os"
	"reflect"
	"regexp"
	"slices"
	"strings"
	"sync"
)

// KnownDiffsFile is the file, next to the fixtures, listing the differences
// the comparison accepts.
const KnownDiffsFile = "known_diffs.json"

// maxValueLen bounds how much of a differing value a Difference shows.
const maxValueLen = 160

// Difference is one place where the EVM-only response departs from v2's.
type Difference struct {
	// Path locates the difference in the response, such as
	// "result.transactions[2].hash", or "error" when only one side failed.
	Path string
	// V2 and EvmOnly are each side's value at Path as JSON, "<absent>" for a
	// missing field, or a summary of the response at the error path.
	V2      string
	EvmOnly string
}

// Display returns both values bounded for a report, starting where they first
// differ when they are too long to show whole.
func (d Difference) Display() (string, string) {
	offset := divergence(d.V2, d.EvmOnly)
	return clipFrom(d.V2, offset), clipFrom(d.EvmOnly, offset)
}

// Compare returns the differences between v2's response and the EVM-only
// response to the same request, ordered by path.
func Compare(v2, evmOnly Response) ([]Difference, error) {
	switch {
	case v2.Error != nil && evmOnly.Error != nil:
		var diffs []Difference
		if v2.Error.Code != evmOnly.Error.Code {
			diffs = append(diffs, Difference{Path: "error.code", V2: fmt.Sprint(v2.Error.Code), EvmOnly: fmt.Sprint(evmOnly.Error.Code)})
		}
		if v2.Error.Message != evmOnly.Error.Message {
			diffs = append(diffs, Difference{Path: "error.message", V2: v2.Error.Message, EvmOnly: evmOnly.Error.Message})
		}
		return diffs, nil
	case v2.Error != nil || evmOnly.Error != nil:
		return []Difference{{Path: "error", V2: describe(v2), EvmOnly: describe(evmOnly)}}, nil
	}
	want, err := decodeJSON(v2.Result)
	if err != nil {
		return nil, fmt.Errorf("decode v2 result: %w", err)
	}
	got, err := decodeJSON(evmOnly.Result)
	if err != nil {
		return nil, fmt.Errorf("decode EVM-only result: %w", err)
	}
	var diffs []Difference
	walk("result", want, got, &diffs)
	return diffs, nil
}

// walk appends the differences between want and got, located under path.
func walk(path string, want, got any, diffs *[]Difference) {
	wantMap, wantIsMap := want.(map[string]any)
	gotMap, gotIsMap := got.(map[string]any)
	if wantIsMap && gotIsMap {
		keys := slices.Sorted(maps.Keys(wantMap))
		for key := range maps.Keys(gotMap) {
			if _, ok := wantMap[key]; !ok {
				keys = append(keys, key)
			}
		}
		slices.Sort(keys)
		for _, key := range keys {
			wantValue, inWant := wantMap[key]
			gotValue, inGot := gotMap[key]
			switch {
			case !inWant:
				*diffs = append(*diffs, Difference{Path: path + "." + key, V2: "<absent>", EvmOnly: encode(gotValue)})
			case !inGot:
				*diffs = append(*diffs, Difference{Path: path + "." + key, V2: encode(wantValue), EvmOnly: "<absent>"})
			default:
				walk(path+"."+key, wantValue, gotValue, diffs)
			}
		}
		return
	}
	wantList, wantIsList := want.([]any)
	gotList, gotIsList := got.([]any)
	if wantIsList && gotIsList {
		if len(wantList) != len(gotList) {
			*diffs = append(*diffs, Difference{Path: path + ".length", V2: fmt.Sprint(len(wantList)), EvmOnly: fmt.Sprint(len(gotList))})
		}
		for i := range min(len(wantList), len(gotList)) {
			walk(fmt.Sprintf("%s[%d]", path, i), wantList[i], gotList[i], diffs)
		}
		return
	}
	if reflect.DeepEqual(want, got) {
		return
	}
	*diffs = append(*diffs, Difference{Path: path, V2: encode(want), EvmOnly: encode(got)})
}

// divergence returns where a and b first differ, moved back to the start of
// the 32-byte hex word it falls in so a long hex value shows whole words.
func divergence(a, b string) int {
	i := 0
	for i < len(a) && i < len(b) && a[i] == b[i] {
		i++
	}
	if strings.HasPrefix(a, `"0x`) && i > 3 {
		return i - (i-3)%hexWordLen
	}
	return i
}

// hexWordLen is the length of a 32-byte word in hex.
const hexWordLen = 64

func decodeJSON(raw json.RawMessage) (any, error) {
	if len(raw) == 0 {
		return nil, nil
	}
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.UseNumber()
	var value any
	if err := decoder.Decode(&value); err != nil {
		return nil, err
	}
	return value, nil
}

// describe summarizes a response for a Difference at the error path.
func describe(response Response) string {
	if response.Error != nil {
		return fmt.Sprintf("error %d: %s", response.Error.Code, response.Error.Message)
	}
	return "result " + string(response.Result)
}

func encode(value any) string {
	bz, err := json.Marshal(value)
	if err != nil {
		return fmt.Sprint(value)
	}
	return string(bz)
}

// clipFrom bounds s to maxValueLen, keeping the text from offset on when s
// is too long to show whole.
func clipFrom(s string, offset int) string {
	if len(s) <= maxValueLen {
		return s
	}
	prefix := ""
	if offset > 0 && offset < len(s) {
		prefix = fmt.Sprintf("…[%d]", offset)
		s = s[offset:]
	}
	if len(s) > maxValueLen {
		s = s[:maxValueLen] + "…"
	}
	return prefix + s
}

// KnownDiff is a difference the comparison accepts, with the reason it is
// accepted. Every field set must match for the entry to cover a difference.
type KnownDiff struct {
	// Method is the JSON-RPC method, or "*" for every method.
	Method string `json:"method"`
	// Path matches a Difference path with every array index written as [*],
	// and every path beneath it.
	Path string `json:"path"`
	// Params, when set, matches each request parameter: "*" matches any value,
	// and anything else must equal the parameter's compact JSON.
	Params []string `json:"params,omitempty"`
	// V2 and EvmOnly, when set, must each be contained in the difference's
	// value on that side.
	V2      string `json:"v2,omitempty"`
	EvmOnly string `json:"evmOnly,omitempty"`
	// Fixtures restricts the entry to the named fixtures; empty means all.
	Fixtures []string `json:"fixtures,omitempty"`
	Reason   string   `json:"reason"`
}

// KnownDiffs is the set of accepted differences, tracking which have matched.
// It is safe for concurrent use.
type KnownDiffs struct {
	mu      sync.Mutex
	entries []KnownDiff
	matched []bool
}

var arrayIndex = regexp.MustCompile(`\[\d+\]`)

// PathPattern returns path with every array index written as [*], the form a
// KnownDiff path matches against.
func PathPattern(path string) string {
	return arrayIndex.ReplaceAllString(path, "[*]")
}

// LoadKnownDiffs reads the accepted differences at path.
func LoadKnownDiffs(path string) (*KnownDiffs, error) {
	bz, err := os.ReadFile(path) //nolint:gosec // path names a checked-in file.
	if err != nil {
		return nil, err
	}
	var entries []KnownDiff
	if err := json.Unmarshal(bz, &entries); err != nil {
		return nil, fmt.Errorf("decode %s: %w", path, err)
	}
	for i, entry := range entries {
		if entry.Method == "" || entry.Path == "" || strings.TrimSpace(entry.Reason) == "" {
			return nil, fmt.Errorf("%s entry %d needs a method, a path, and a reason", path, i)
		}
	}
	return &KnownDiffs{entries: entries, matched: make([]bool, len(entries))}, nil
}

// Accepts reports whether a known difference covers diff in the response to
// request in fixture, and records every entry that does.
func (k *KnownDiffs) Accepts(fixture string, request Request, diff Difference) bool {
	params := compactParams(request.Params)
	k.mu.Lock()
	defer k.mu.Unlock()
	accepted := false
	for i, entry := range k.entries {
		if entry.covers(fixture, request.Method, params, diff) {
			k.matched[i] = true
			accepted = true
		}
	}
	return accepted
}

// Unmatched returns the entries that no difference has matched.
func (k *KnownDiffs) Unmatched() []KnownDiff {
	k.mu.Lock()
	defer k.mu.Unlock()
	var unmatched []KnownDiff
	for i, entry := range k.entries {
		if !k.matched[i] {
			unmatched = append(unmatched, entry)
		}
	}
	return unmatched
}

func (e KnownDiff) covers(fixture, method string, params []string, diff Difference) bool {
	if e.Method != "*" && e.Method != method {
		return false
	}
	if len(e.Fixtures) > 0 && !slices.Contains(e.Fixtures, fixture) {
		return false
	}
	if !strings.Contains(diff.V2, e.V2) || !strings.Contains(diff.EvmOnly, e.EvmOnly) {
		return false
	}
	if e.Params != nil && !matchParams(e.Params, params) {
		return false
	}
	path := PathPattern(diff.Path)
	return path == e.Path || strings.HasPrefix(path, e.Path+".") || strings.HasPrefix(path, e.Path+"[")
}

func matchParams(patterns, params []string) bool {
	if len(patterns) != len(params) {
		return false
	}
	for i, pattern := range patterns {
		if pattern != "*" && pattern != params[i] {
			return false
		}
	}
	return true
}

// FormatParams renders params as a compact, parenthesized argument list.
func FormatParams(params []json.RawMessage) string {
	return "(" + strings.Join(compactParams(params), ", ") + ")"
}

// compactParams returns each parameter as compact JSON.
func compactParams(params []json.RawMessage) []string {
	compacted := make([]string, len(params))
	for i, param := range params {
		var buf bytes.Buffer
		if err := json.Compact(&buf, param); err != nil {
			compacted[i] = string(param)
			continue
		}
		compacted[i] = buf.String()
	}
	return compacted
}
