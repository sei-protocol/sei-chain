package ratelimiter

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

func parseSingle(t *testing.T, body string) string {
	t.Helper()
	methods, batch, err := NewMethodParser(0).Parse(strings.NewReader(body))
	require.NoError(t, err)
	require.False(t, batch)
	require.Len(t, methods, 1)
	return methods[0]
}

// --- single request, EVM + CometBFT framings ---

func TestParse_EVMRequest(t *testing.T) {
	body := `{"jsonrpc":"2.0","id":1,"method":"eth_call","params":[{"to":"0xabc","data":"0x00"},"latest"]}`
	require.Equal(t, "eth_call", parseSingle(t, body))
}

func TestParse_CometBFTRequest(t *testing.T) {
	body := `{"jsonrpc":"2.0","id":"abc","method":"abci_query","params":{"path":"/store/x/subspace","data":"deadbeef"}}`
	require.Equal(t, "abci_query", parseSingle(t, body))
}

func TestParse_MethodBeforeParams(t *testing.T) {
	// Field order both go-ethereum and CometBFT emit: method ahead of params.
	body := `{"method":"eth_getLogs","params":[{"fromBlock":"0x0"}],"id":1,"jsonrpc":"2.0"}`
	require.Equal(t, "eth_getLogs", parseSingle(t, body))
}

func TestParse_MethodAfterParams(t *testing.T) {
	// Order is not guaranteed by JSON; a huge params before method must still work.
	body := `{"jsonrpc":"2.0","params":[1,2,3,{"nested":{"a":[true,false,null]}}],"id":7,"method":"eth_chainId"}`
	require.Equal(t, "eth_chainId", parseSingle(t, body))
}

func TestParse_EmptyParams(t *testing.T) {
	require.Equal(t, "net_version", parseSingle(t, `{"jsonrpc":"2.0","id":1,"method":"net_version","params":[]}`))
}

func TestParse_NoParamsField(t *testing.T) {
	require.Equal(t, "eth_blockNumber", parseSingle(t, `{"jsonrpc":"2.0","id":1,"method":"eth_blockNumber"}`))
}

func TestParse_LeadingWhitespace(t *testing.T) {
	require.Equal(t, "eth_call", parseSingle(t, "  \n\t {\"method\":\"eth_call\",\"id\":1}"))
}

func TestParse_EscapedMethodName(t *testing.T) {
	// The decoder unescapes the string value, as a full decode would.
	require.Equal(t, "weird\"name", parseSingle(t, `{"method":"weird\"name","id":1}`))
}

func TestParse_NestedParamsSkipped(t *testing.T) {
	body := `{"params":{"a":{"b":{"c":[1,[2,[3]]]}},"d":"x"},"method":"trace_block","id":1}`
	require.Equal(t, "trace_block", parseSingle(t, body))
}

func TestParse_DuplicateMethodRejected(t *testing.T) {
	// encoding/json (used by the downstream handlers) keeps the last value for a
	// duplicate key, so charging the first would be a rate-limit bypass; reject.
	_, _, err := NewMethodParser(0).Parse(strings.NewReader(`{"method":"first","x":1,"method":"second"}`))
	require.ErrorIs(t, err, ErrDuplicateMethod)
}

func TestParse_BatchDuplicateMethodRejected(t *testing.T) {
	body := `[{"method":"eth_call"},{"method":"a","method":"b"}]`
	_, _, err := NewMethodParser(0).Parse(strings.NewReader(body))
	require.ErrorIs(t, err, ErrDuplicateMethod)
}

// --- batch ---

func TestParse_Batch(t *testing.T) {
	body := `[
		{"jsonrpc":"2.0","id":1,"method":"eth_call","params":[{},"latest"]},
		{"jsonrpc":"2.0","id":2,"method":"eth_getLogs","params":[{"fromBlock":"0x0"}]},
		{"jsonrpc":"2.0","id":3,"method":"eth_blockNumber"}
	]`
	methods, batch, err := NewMethodParser(0).Parse(strings.NewReader(body))
	require.NoError(t, err)
	require.True(t, batch)
	require.Equal(t, []string{"eth_call", "eth_getLogs", "eth_blockNumber"}, methods)
}

func TestParse_BatchSingleElement(t *testing.T) {
	// A one-element array is still a batch (framing differs from a bare object).
	methods, batch, err := NewMethodParser(0).Parse(strings.NewReader(`[{"method":"eth_call","id":1}]`))
	require.NoError(t, err)
	require.True(t, batch)
	require.Equal(t, []string{"eth_call"}, methods)
}

func TestParse_BatchMethodAfterParams(t *testing.T) {
	body := `[{"params":[{"big":[1,2,3]}],"method":"m1"},{"params":{"x":1},"method":"m2"}]`
	methods, batch, err := NewMethodParser(0).Parse(strings.NewReader(body))
	require.NoError(t, err)
	require.True(t, batch)
	require.Equal(t, []string{"m1", "m2"}, methods)
}

// --- errors ---

func TestParse_EmptyBatch(t *testing.T) {
	_, batch, err := NewMethodParser(0).Parse(strings.NewReader(`[]`))
	require.ErrorIs(t, err, ErrEmptyBatch)
	require.True(t, batch)
}

func TestParse_NoMethodField(t *testing.T) {
	_, _, err := NewMethodParser(0).Parse(strings.NewReader(`{"jsonrpc":"2.0","id":1,"params":[]}`))
	require.ErrorIs(t, err, ErrNoMethod)
}

func TestParse_MethodNotString(t *testing.T) {
	_, _, err := NewMethodParser(0).Parse(strings.NewReader(`{"method":123,"id":1}`))
	require.ErrorIs(t, err, ErrMethodNotString)
}

func TestParse_TopLevelScalar(t *testing.T) {
	_, _, err := NewMethodParser(0).Parse(strings.NewReader(`"eth_call"`))
	require.ErrorIs(t, err, ErrNotObject)
}

func TestParse_TopLevelNumber(t *testing.T) {
	_, _, err := NewMethodParser(0).Parse(strings.NewReader(`42`))
	require.ErrorIs(t, err, ErrNotObject)
}

func TestParse_BatchElementNotObject(t *testing.T) {
	_, _, err := NewMethodParser(0).Parse(strings.NewReader(`[1, 2, 3]`))
	require.ErrorIs(t, err, ErrNotObject)
}

func TestParse_TruncatedBatchRejected(t *testing.T) {
	// Missing closing ']'. dec.More() reports false at EOF without erroring, so
	// the malformed body must be caught by validating the closing delim — and it
	// is a parse error, not a probe-limit hit (the budget was never exhausted).
	_, _, err := NewMethodParser(0).Parse(strings.NewReader(`[{"method":"eth_call"}`))
	require.Error(t, err)
	require.NotErrorIs(t, err, ErrProbeLimit)
}

func TestParse_EmptyBody(t *testing.T) {
	_, _, err := NewMethodParser(0).Parse(strings.NewReader(``))
	require.Error(t, err)
	require.NotErrorIs(t, err, ErrProbeLimit)
}

func TestParse_MalformedJSON(t *testing.T) {
	_, _, err := NewMethodParser(0).Parse(strings.NewReader(`{"method":`))
	require.Error(t, err)
}

func TestParse_TruncatedBodyNotProbeLimit(t *testing.T) {
	// A short body that ends before "method" is a genuinely truncated request,
	// not a probe-limit hit — the budget was never exhausted.
	_, _, err := NewMethodParser(0).Parse(strings.NewReader(`{"params":[1,2,3]`))
	require.Error(t, err)
	require.NotErrorIs(t, err, ErrProbeLimit)
}

// --- probe limit / partial read ---

func TestParse_ProbeLimitExceeded(t *testing.T) {
	// "method" sits after a params array larger than the probe budget.
	body := `{"params":[` + strings.Repeat("1,", 500) + `1],"method":"eth_call"}`
	_, _, err := NewMethodParser(64).Parse(strings.NewReader(body))
	require.ErrorIs(t, err, ErrProbeLimit)
}

func TestParse_LargeTrailingParamsHitProbeLimit(t *testing.T) {
	// The whole object is read (bounded by the probe) so a trailing duplicate
	// "method" can be rejected; a params array larger than a tiny probe therefore
	// yields ErrProbeLimit even though "method" appears first.
	body := `{"method":"eth_call","params":[` + strings.Repeat("9,", 100_000) + `9]}`
	_, _, err := NewMethodParser(128).Parse(strings.NewReader(body))
	require.ErrorIs(t, err, ErrProbeLimit)
}

func TestParse_DefaultProbeLimitApplied(t *testing.T) {
	require.Equal(t, int64(DefaultMaxProbeBytes), NewMethodParser(0).maxProbeBytes)
	require.Equal(t, int64(DefaultMaxProbeBytes), NewMethodParser(-5).maxProbeBytes)
	require.Equal(t, int64(256), NewMethodParser(256).maxProbeBytes)
}

func TestParse_LargeParamsWithinDefaultProbe(t *testing.T) {
	// A realistic ~200 KiB params well under the 1 MiB default probe: method
	// found regardless of whether it precedes or follows params.
	big := strings.Repeat("a", 200*1024)
	body := `{"jsonrpc":"2.0","id":1,"params":["0x` + big + `"],"method":"eth_sendRawTransaction"}`
	require.Equal(t, "eth_sendRawTransaction", parseSingle(t, body))
}

// --- reader-only interface: Parse consumes r, callers rewind/tee separately ---

func TestParse_ReadsFromReader(t *testing.T) {
	r := strings.NewReader(`{"method":"eth_call","id":1,"params":[]}`)
	methods, _, err := NewMethodParser(0).Parse(r)
	require.NoError(t, err)
	require.Equal(t, []string{"eth_call"}, methods)
}
