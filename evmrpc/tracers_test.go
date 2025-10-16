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
	fmt.Println(resObj)
	result := resObj["result"].(map[string]interface{})
	require.Equal(t, "0x5b4eba929f3811980f5ae0c5d04fa200f837df4e", result["from"])
	require.Equal(t, "0x30d40", result["gas"])
	require.Equal(t, "0x616263", result["input"])
	require.Equal(t, "0x0000000000000000000000000000000000010203", result["to"])
	require.Equal(t, "CALL", result["type"])
	require.Equal(t, "0x3e8", result["value"])

	// test prestateTracer
	args["tracer"] = "prestateTracer"
	resObj = sendRequestGoodWithNamespace(t, "debug", "traceTransaction", DebugTraceHashHex, args)
	result = resObj["result"].(map[string]interface{})
	for _, v := range result {
		require.Contains(t, v, "balance")
		balanceMap := v.(map[string]interface{})
		balance := balanceMap["balance"].(string)
		require.Greater(t, len(balance), 2)
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

	resObj := sendRequestGoodWithNamespace(t, "debug", "traceCall", txArgs, "0x65")
	result := resObj["result"].(map[string]interface{})
	require.Equal(t, float64(21000), result["gas"])
	require.Equal(t, false, result["failed"])
}

func TestTraceTransactionTimeout(t *testing.T) {
	args := map[string]interface{}{"tracer": "callTracer"}

	resObj := sendRequestStrictWithNamespace(
		t,
		"debug",
		"traceTransaction",
		DebugTraceHashHex,
		args,
	)

	errObj, ok := resObj["error"].(map[string]interface{})
	require.True(t, ok, "expected node‑level timeout to trigger")
	require.NotEmpty(t, errObj["message"].(string))
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

func TestTraceTransactionValidation(t *testing.T) {
	// Test tracing a transaction that exists
	args := map[string]interface{}{"tracer": "callTracer"}
	resObj := sendRequestGoodWithNamespace(t, "debug", "traceTransaction", DebugTraceHashHex, args)

	// Should have a result, not an error
	_, hasResult := resObj["result"]
	_, hasError := resObj["error"]
	require.True(t, hasResult, "expected successful trace result")
	require.False(t, hasError, "expected no error for valid transaction")
}

func TestTraceTransactionNotFound(t *testing.T) {
	// Test tracing a non-existent transaction hash
	nonExistentTxHash := "0x1111111111111111111111111111111111111111111111111111111111111111"
	args := map[string]interface{}{"tracer": "callTracer"}

	resObj := sendRequestGoodWithNamespace(t, "debug", "traceTransaction", nonExistentTxHash, args)

	// Should have an error
	errObj, hasError := resObj["error"].(map[string]interface{})
	require.True(t, hasError, "expected error for non-existent transaction")

	// Check that the error message indicates transaction not found
	message, hasMessage := errObj["message"].(string)
	require.True(t, hasMessage, "expected error message")
	require.Contains(t, message, "failed to get transaction", "expected transaction not found error")
}

func TestTraceBlockByHashValidation(t *testing.T) {
	// Test with the existing debug trace block hash
	args := map[string]interface{}{"tracer": "callTracer"}
	resObj := sendRequestGoodWithNamespace(t, "debug", "traceBlockByHash", DebugTraceBlockHash, args)

	// Should have a result, not an error
	_, hasResult := resObj["result"]
	_, hasError := resObj["error"]
	require.True(t, hasResult, "expected successful trace result")
	require.False(t, hasError, "expected no error for valid block hash")
}

func TestTraceBlockByHashWithStrictLimits(t *testing.T) {
	// Test with strict server that has limited lookback
	args := map[string]interface{}{"tracer": "callTracer"}

	// Use a block hash that would be beyond the lookback limit
	resObj := sendRequestStrictWithNamespace(t, "debug", "traceBlockByHash", DebugTraceBlockHash, args)

	// This might result in an error depending on the block height and lookback settings
	if errObj, hasError := resObj["error"].(map[string]interface{}); hasError {
		message := errObj["message"].(string)
		// Should be a lookback-related error
		require.True(t,
			strings.Contains(message, "beyond max lookback") || strings.Contains(message, "height not available"),
			"expected lookback or availability error, got: %s", message)
	}
}
