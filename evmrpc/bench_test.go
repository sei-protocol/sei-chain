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

// Timed mode: the arm stamps the timer precompile twice around 100
// JUMPDESTs and returns 7. The gate must see the return data and the
// stamped gas (128 per stamp sequence + 100 of section + a cold timer
// address, or 2500 less if the node pre-warms it) and every round's nsA/nsB
// must be the stamped interval, one call per round.
func TestBenchProgramTimed(t *testing.T) {
	const k = 100
	stamp := []byte{
		0x5a,             // GAS
		0x60, 0x20, 0x52, // PUSH1 32 MSTORE  (mem[32..64] = gas)
		0x60, 0x00, 0x60, 0x00, 0x60, 0x20, 0x60, 0x20, // retLen retOff argsLen argsOff
		0x61, 0xc1, 0x0c, // PUSH2 timer address
		0x62, 0x01, 0x86, 0xa0, // PUSH3 100000 gas
		0xfa, 0x50, // STATICCALL POP
	}
	var code []byte
	code = append(code, 0x60, 0x00, 0x60, 0x3f, 0x53) // PUSH1 0 PUSH1 63 MSTORE8: expand to 64 B outside the stamps
	code = append(code, stamp...)
	for i := 0; i < k; i++ {
		code = append(code, 0x5b)
	}
	code = append(code, stamp...)
	code = append(code, 0x60, 0x07, 0x60, 0x00, 0x52, 0x60, 0x20, 0x60, 0x00, 0xf3) // return 7
	want := make([]byte, 32)
	want[31] = 7
	arm := map[string]interface{}{"code": "0x" + hex.EncodeToString(code), "calldata": "0x", "gas": 0,
		"wantOut": "0x" + hex.EncodeToString(want), "wantTimedGas": 128 + 2500 + k}
	req := map[string]interface{}{
		"name":       "timed/jumpdest100",
		"a":          arm,
		"b":          arm,
		"n":          k,
		"opGas":      1,
		"gate":       "equal",
		"rounds":     4,
		"batchNs":    100000,
		"pinCpu":     -1,
		"timed":      true,
		"roundGapNs": 1e6,
	}
	res := sendRequestGoodWithNamespace(t, "debug", "benchProgram", req)
	require.Nil(t, res["error"], "%v", res)
	r := res["result"].(map[string]interface{})
	require.Equal(t, true, r["gateOk"], "%v", r["gateNote"])
	require.Equal(t, r["gasA"], r["gasB"])
	tg := r["timedGasA"].(float64)
	require.Contains(t, []float64{128 + 2500 + k, 128 + k}, tg, "timed gas %v (note %v)", tg, r["gateNote"])
	require.EqualValues(t, 1, r["itersA"])
	require.EqualValues(t, 1, r["itersB"])
	require.Len(t, r["rounds"].([]interface{}), 4)
	first := r["rounds"].([]interface{})[0].(map[string]interface{})
	require.Greater(t, first["nsA"].(float64), 0.0)
	require.Greater(t, first["nsB"].(float64), 0.0)
	require.Less(t, first["nsA"].(float64), 1e7) // the stamped section, not the whole call
}

// Timed mode with a wrong expectation must fail the gate.
func TestBenchProgramTimedGateFails(t *testing.T) {
	arm := map[string]interface{}{"code": "0x00", "calldata": "0x", "gas": 0, "wantOut": "0x", "wantTimedGas": 1}
	req := map[string]interface{}{"name": "timed/bad", "a": arm, "b": arm, "n": 1, "opGas": 1, "gate": "equal",
		"rounds": 2, "batchNs": 100000, "pinCpu": -1, "timed": true}
	res := sendRequestGoodWithNamespace(t, "debug", "benchProgram", req)
	r := res["result"].(map[string]interface{})
	require.Equal(t, false, r["gateOk"])
	require.Contains(t, r["gateNote"].(string), "timer stamped 0 times")
}
