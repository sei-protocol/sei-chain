package evmrpc_test

import (
	"fmt"
	"strings"
	"testing"

	testkeeper "github.com/sei-protocol/sei-chain/testutil/keeper"
	"github.com/stretchr/testify/require"
)

func TestTraceTransaction(t *testing.T) {
	args := map[string]interface{}{}

	// test callTracer
	args["tracer"] = "callTracer"
	resObj := sendRequestGoodWithNamespace(t, "debug", "traceTransaction", DebugTraceHashHex, args)
	result := resObj["result"].(map[string]interface{})
	require.Equal(t, "0x5b4eba929f3811980f5ae0c5d04fa200f837df4e", strings.ToLower(result["from"].(string)))
	require.Equal(t, "0x3e8", result["gas"])
	require.Equal(t, "0x", result["input"])
	require.Contains(t, result["error"].(string), "intrinsic gas too low")
	require.Equal(t, "0x0000000000000000000000000000000000010203", result["to"])
	if callType, ok := result["type"]; ok {
		require.Equal(t, "CALL", callType)
	}
	require.Equal(t, "0x3e8", result["value"])

	// test prestateTracer
	args["tracer"] = "prestateTracer"
	resObj = sendRequestGoodWithNamespace(t, "debug", "traceTransaction", DebugTraceHashHex, args)
	if errObj, ok := resObj["error"].(map[string]interface{}); ok {
		require.Contains(t, errObj["message"].(string), "tracing failed", "prestate tracer should propagate failures")
	} else {
		result = resObj["result"].(map[string]interface{})
		for _, v := range result {
			require.Contains(t, v, "balance")
			balanceMap := v.(map[string]interface{})
			balance := balanceMap["balance"].(string)
			require.Greater(t, len(balance), 2)
		}
	}
}

func TestTraceCall(t *testing.T) {
	_, contractAddr := testkeeper.MockAddressPair()
	txArgs := map[string]interface{}{
		"from":         "0x5B4eba929F3811980f5AE0c5D04fa200f837DF4E",
		"to":           contractAddr.Hex(),
		"chainId":      fmt.Sprintf("%#x", EVMKeeper.ChainID(Ctx)),
		"maxFeePerGas": "0x3B9ACA00",
	}

	resObj := sendRequestGoodWithNamespace(t, "debug", "traceCall", txArgs, fmt.Sprintf("0x%x", MockHeight8))
	result := resObj["result"].(map[string]interface{})
	require.Equal(t, float64(21000), result["gas"])
	require.Equal(t, false, result["failed"])
}

func TestTraceTransactionTimeout(t *testing.T) {
	args := map[string]interface{}{"tracer": "callTracer", "timeout": "1ns"}
	resObj := sendRequestStrictWithNamespace(
		t,
		"debug",
		"traceTransaction",
		DebugTraceHashHex,
		args,
	)

	if errObj, ok := resObj["error"].(map[string]interface{}); ok {
		require.NotEmpty(t, errObj["message"])
		return
	}
	result := resObj["result"].(map[string]interface{})
	errMsg, ok := result["error"].(string)
	require.True(t, ok, "expected tracer error payload")
	require.NotEmpty(t, errMsg)
}

func TestTraceBlockByNumberLookbackLimit(t *testing.T) {
	// Using the strict server (look‑back = 1). Block 0 is far behind.
	resObj := sendRequestStrictWithNamespace(
		t,
		"sei",
		"traceBlockByNumberExcludeTraceFail",
		"0x0",                    // genesis block
		map[string]interface{}{}, // empty TraceConfig
	)

	errObj, ok := resObj["error"].(map[string]interface{})
	require.True(t, ok, "expected look‑back guard to trigger")
	require.NotEmpty(t, errObj["message"].(string))
}

func TestTraceBlockByNumberUnlimitedLookback(t *testing.T) {
	// Using the archive server (look‑back = -1). Block 0 should be accessible.
	resObj := sendRequestArchiveWithNamespace(
		t,
		"sei",
		"traceBlockByNumberExcludeTraceFail",
		"0x0",                    // genesis block
		map[string]interface{}{}, // empty TraceConfig
	)

	_, ok := resObj["error"]
	require.False(t, ok, "expected look-back to be unlimited")
	_, ok = resObj["result"]
	require.True(t, ok, "expected result to be present")
}
