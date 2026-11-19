package evmrpc_test

import (
	"encoding/hex"
	"testing"

	"github.com/stretchr/testify/require"
)

// Two programs that differ only in sixty-four JUMPDESTs: the gate must see
// gasA − gasB == 64 × 1, and every requested round must come back.
func TestBenchProgramDiff(t *testing.T) {
	a := make([]byte, 0, 65)
	for i := 0; i < 64; i++ {
		a = append(a, 0x5b) // JUMPDEST, 1 gas
	}
	a = append(a, 0x00) // STOP
	req := map[string]interface{}{
		"name":    "jumpdest/n64",
		"a":       map[string]interface{}{"code": "0x" + hex.EncodeToString(a), "calldata": "0x", "gas": 64},
		"b":       map[string]interface{}{"code": "0x00", "calldata": "0x", "gas": 0},
		"n":       64,
		"opGas":   1,
		"gate":    "diff",
		"rounds":  4,
		"batchNs": 200000,
		"pinCpu":  -1,
	}
	res := sendRequestGoodWithNamespace(t, "debug", "benchProgram", req)
	require.Nil(t, res["error"], "%v", res)
	r := res["result"].(map[string]interface{})
	require.Equal(t, true, r["gateOk"], "%v", r["gateNote"])
	require.EqualValues(t, 64, r["gasA"])
	require.EqualValues(t, 0, r["gasB"])
	require.Len(t, r["rounds"].([]interface{}), 4)
	require.Greater(t, r["itersA"].(float64), 0.0)
	require.Greater(t, r["itersB"].(float64), 0.0)
	first := r["rounds"].([]interface{})[0].(map[string]interface{})
	require.Greater(t, first["nsA"].(float64), 0.0)
	require.Greater(t, first["nsB"].(float64), 0.0)
}

// A wrong analytic gas must fail the gate, not be measured.
func TestBenchProgramGateFails(t *testing.T) {
	req := map[string]interface{}{
		"name":    "bad-gas",
		"a":       map[string]interface{}{"code": "0x5b00", "calldata": "0x", "gas": 7},
		"b":       map[string]interface{}{"code": "0x00", "calldata": "0x", "gas": 0},
		"n":       1,
		"opGas":   1,
		"gate":    "diff",
		"rounds":  2,
		"batchNs": 100000,
		"pinCpu":  -1,
	}
	res := sendRequestGoodWithNamespace(t, "debug", "benchProgram", req)
	r := res["result"].(map[string]interface{})
	require.Equal(t, false, r["gateOk"])
	require.Contains(t, r["gateNote"].(string), "FAIL arm A gas")
	require.Nil(t, r["rounds"])
}

// The identity precompile: output must equal the input and RequiredGas
// must match the client's expectation (15 + 3 per word).
func TestBenchProgramPrecompile(t *testing.T) {
	in := make([]byte, 32)
	for i := range in {
		in[i] = byte(i)
	}
	req := map[string]interface{}{
		"name":       "identity/32",
		"precompile": map[string]interface{}{"addr": "0x0000000000000000000000000000000000000004", "input": "0x" + hex.EncodeToString(in), "wantOut": "0x" + hex.EncodeToString(in), "wantGas": 18},
		"n":          1,
		"opGas":      18,
		"gate":       "abs",
		"rounds":     3,
		"batchNs":    100000,
		"pinCpu":     -1,
	}
	res := sendRequestGoodWithNamespace(t, "debug", "benchProgram", req)
	r := res["result"].(map[string]interface{})
	require.Equal(t, true, r["gateOk"], "%v", r["gateNote"])
	require.EqualValues(t, 18, r["gasA"])
	require.Len(t, r["rounds"].([]interface{}), 3)
}
