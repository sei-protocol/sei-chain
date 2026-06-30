package evmrpc_test

import (
	"fmt"
	"strings"
	"testing"

	"github.com/sei-protocol/sei-chain/evmrpc"
	testkeeper "github.com/sei-protocol/sei-chain/testutil/keeper"
	"github.com/stretchr/testify/require"
)

func TestDebugGetRawMethodsNotSupported(t *testing.T) {
	for _, method := range []string{"getRawBlock", "getRawHeader", "getRawReceipts"} {
		m := method
		t.Run(m, func(t *testing.T) {
			resObj := sendRequestGoodWithNamespace(t, "debug", m, "latest")
			require.Contains(t, resObj, "error", m)
			errObj := resObj["error"].(map[string]interface{})
			require.Equal(t, float64(evmrpc.ErrCodeEVMNotSupported), errObj["code"], m)
			require.Contains(t, errObj["message"].(string), "debug_"+m)
		})
	}
	t.Run("getRawTransaction", func(t *testing.T) {
		resObj := sendRequestGoodWithNamespace(t, "debug", "getRawTransaction", DebugTraceHashHex)
		require.Contains(t, resObj, "error")
		errObj := resObj["error"].(map[string]interface{})
		require.Equal(t, float64(evmrpc.ErrCodeEVMNotSupported), errObj["code"])
		require.Contains(t, errObj["message"].(string), "debug_getRawTransaction")
	})
}

func TestTraceTransaction(t *testing.T) {
	args := map[string]interface{}{}

	// test callTracer
	args["tracer"] = "callTracer"
	resObj := sendRequestGoodWithNamespace(t, "debug", "traceTransaction", DebugTraceHashHex, args)
	result := resObj["result"].(map[string]interface{})
	require.Equal(t, "0x5b4eba929f3811980f5ae0c5d04fa200f837df4e", strings.ToLower(result["from"].(string)))
	require.Equal(t, "0x3e8", result["gas"])
	require.Equal(t, "0x616263", result["input"]) // hex of "abc" (Data field in test tx)
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

// TestTraceStateAccessTimeout asserts that debug_traceStateAccess honors the
// configured TraceTimeout via prepareTraceContext: the strict server uses a
// 1ns TraceTimeout, so the context expires before the replay loop completes
// and the call fails with a deadline error rather than running unbounded.
func TestTraceStateAccessTimeout(t *testing.T) {
	resObj := sendRequestStrictWithNamespace(
		t,
		"debug",
		"traceStateAccess",
		DebugTraceHashHex,
	)

	errObj, ok := resObj["error"].(map[string]interface{})
	require.True(t, ok, "expected error from strict (1ns timeout) server, got: %v", resObj)
	require.Contains(t, errObj["message"], "context deadline exceeded")
}
