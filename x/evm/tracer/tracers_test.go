package evmrpc_test

import (
	"fmt"
	"testing"
        "time"

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

        // Simulate slow node response to trigger timeout
        time.Sleep(2 * time.Second)

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
