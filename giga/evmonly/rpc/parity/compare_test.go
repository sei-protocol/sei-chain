package parity

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"
)

func result(t *testing.T, value string) Response {
	t.Helper()
	require.True(t, json.Valid([]byte(value)), value)
	return Response{Result: json.RawMessage(value)}
}

func TestCompareResults(t *testing.T) {
	v2 := result(t, `{"a":"0x1","b":[1,2,3],"c":{"d":null},"only":true}`)
	evmOnly := result(t, `{"a":"0x1","b":[1,5],"c":{"d":"0x0"},"extra":1}`)
	diffs, err := Compare(v2, evmOnly)
	require.NoError(t, err)
	require.Equal(t, []Difference{
		{Path: "result.b.length", V2: "3", EvmOnly: "2"},
		{Path: "result.b[1]", V2: "2", EvmOnly: "5"},
		{Path: "result.c.d", V2: "null", EvmOnly: `"0x0"`},
		{Path: "result.extra", V2: "<absent>", EvmOnly: "1"},
		{Path: "result.only", V2: "true", EvmOnly: "<absent>"},
	}, diffs)

	diffs, err = Compare(v2, v2)
	require.NoError(t, err)
	require.Empty(t, diffs)
}

func TestCompareErrors(t *testing.T) {
	failed := Response{Error: &RPCError{Code: -32000, Message: "not found"}}
	diffs, err := Compare(failed, result(t, "null"))
	require.NoError(t, err)
	require.Equal(t, []Difference{{Path: "error", V2: "error -32000: not found", EvmOnly: "result null"}}, diffs)

	diffs, err = Compare(failed, Response{Error: &RPCError{Code: -32601, Message: "not found"}})
	require.NoError(t, err)
	require.Equal(t, []Difference{{Path: "error.code", V2: "-32000", EvmOnly: "-32601"}}, diffs)
}

func TestDifferenceDisplayStartsAtDivergingWord(t *testing.T) {
	word := func(c string) string {
		out := ""
		for range hexWordLen {
			out += c
		}
		return out
	}
	diff := Difference{
		V2:      `"0x` + word("0") + word("0") + word("0") + word("a") + `"`,
		EvmOnly: `"0x` + word("0") + word("0") + word("0") + word("b") + `"`,
	}
	v2, evmOnly := diff.Display()
	require.Equal(t, "…[195]"+word("a")+`"`, v2)
	require.Equal(t, "…[195]"+word("b")+`"`, evmOnly)
}

func TestKnownDiffsMatchAndTrack(t *testing.T) {
	path := filepath.Join(t.TempDir(), KnownDiffsFile)
	require.NoError(t, os.WriteFile(path, []byte(`[
		{"method": "eth_getBlockByNumber", "path": "result.transactions[*].gasPrice", "reason": "per-block"},
		{"method": "eth_getBalance", "path": "result", "params": ["\"0xc0\"", "*"], "reason": "one account"},
		{"method": "*", "path": "error", "evmOnly": "not supported", "fixtures": ["a"], "reason": "unsupported"},
		{"method": "eth_chainId", "path": "result", "reason": "never happens"}
	]`), 0o600))
	known, err := LoadKnownDiffs(path)
	require.NoError(t, err)

	request := func(method string, params ...string) Request {
		raw := make([]json.RawMessage, len(params))
		for i, param := range params {
			raw[i] = json.RawMessage(param)
		}
		return Request{Method: method, Params: raw}
	}
	gasPrice := Difference{Path: "result.transactions[3].gasPrice"}
	require.True(t, known.Accepts("a", request("eth_getBlockByNumber", `"0x2"`, "true"), gasPrice))
	require.False(t, known.Accepts("a", request("eth_getBlockByHash", `"0x2"`, "true"), gasPrice))
	require.False(t, known.Accepts("a", request("eth_getBlockByNumber"), Difference{Path: "result.transactions[3].gasPriceExtra"}))

	require.True(t, known.Accepts("a", request("eth_getBalance", ` "0xc0" `, `"latest"`), Difference{Path: "result"}))
	require.False(t, known.Accepts("a", request("eth_getBalance", `"0xc1"`, `"latest"`), Difference{Path: "result"}))

	unsupported := Difference{Path: "error", EvmOnly: "error -32000: historical state is not supported"}
	require.True(t, known.Accepts("a", request("eth_getCode"), unsupported))
	require.False(t, known.Accepts("b", request("eth_getCode"), unsupported))
	require.False(t, known.Accepts("a", request("eth_getCode"), Difference{Path: "error", EvmOnly: "result null"}))

	unmatched := known.Unmatched()
	require.Len(t, unmatched, 1)
	require.Equal(t, "eth_chainId", unmatched[0].Method)
}

func TestLoadKnownDiffsRequiresReason(t *testing.T) {
	path := filepath.Join(t.TempDir(), KnownDiffsFile)
	require.NoError(t, os.WriteFile(path, []byte(`[{"method": "eth_chainId", "path": "result", "reason": " "}]`), 0o600))
	_, err := LoadKnownDiffs(path)
	require.ErrorContains(t, err, "reason")
}
