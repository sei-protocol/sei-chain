package evmrpc_test

import (
	"fmt"
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
	require.Equal(t, "0x5b4eba929f3811980f5ae0c5d04fa200f837df4e", result["from"])
	require.Equal(t, "0x55f0", result["gas"])
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

func TestTraceBlockByNumber(t *testing.T) {
	args := map[string]interface{}{}
	// test callTracer
	args["tracer"] = "callTracer"
	resObj := sendRequestGoodWithNamespace(t, "debug", "traceBlockByNumber", "0x65", args)
	result := resObj["result"].([]interface{})[0].(map[string]interface{})["result"].(map[string]interface{})
	require.Equal(t, "0x5b4eba929f3811980f5ae0c5d04fa200f837df4e", result["from"])
	require.Equal(t, "0x55f0", result["gas"])
	require.Equal(t, "0x616263", result["input"])
	require.Equal(t, "0x0000000000000000000000000000000000010203", result["to"])
	require.Equal(t, "CALL", result["type"])
	require.Equal(t, "0x3e8", result["value"])
	args["tracer"] = "prestateTracer"
	resObj = sendRequestGoodWithNamespace(t, "debug", "traceBlockByNumber", "0x65", args)
	result = resObj["result"].([]interface{})[0].(map[string]interface{})["result"].(map[string]interface{})
	require.Equal(t, 3, len(result))
}

func TestTraceBlockByHash(t *testing.T) {
	args := map[string]interface{}{}
	// test callTracer
	args["tracer"] = "callTracer"
	resObj := sendRequestGoodWithNamespace(t, "debug", "traceBlockByHash", DebugTraceBlockHash, args)
	result := resObj["result"].([]interface{})[0].(map[string]interface{})["result"].(map[string]interface{})
	require.Equal(t, "0x5b4eba929f3811980f5ae0c5d04fa200f837df4e", result["from"])
	require.Equal(t, "0x55f0", result["gas"])
	require.Equal(t, "0x616263", result["input"])
	require.Equal(t, "0x0000000000000000000000000000000000010203", result["to"])
	require.Equal(t, "CALL", result["type"])
	require.Equal(t, "0x3e8", result["value"])

	// test prestateTracer
	args["tracer"] = "prestateTracer"
	resObj = sendRequestGoodWithNamespace(t, "debug", "traceBlockByHash", DebugTraceBlockHash, args)
	result = resObj["result"].([]interface{})[0].(map[string]interface{})["result"].(map[string]interface{})
	require.Equal(t, 3, len(result))
}

func TestTraceCall(t *testing.T) {
	_, from := testkeeper.MockAddressPair()
	_, contractAddr := testkeeper.MockAddressPair()
	txArgs := map[string]interface{}{
		"from":    from.Hex(),
		"to":      contractAddr.Hex(),
		"chainId": fmt.Sprintf("%#x", EVMKeeper.ChainID(Ctx)),
	}

	resObj := sendRequestGoodWithNamespace(t, "debug", "traceCall", txArgs, "0x65")
	result := resObj["result"].(map[string]interface{})
	require.Equal(t, float64(21000), result["gas"])
	require.Equal(t, false, result["failed"])
}

func TestTraceBlockByNumberExcludeTraceFail(t *testing.T) {
	args := map[string]interface{}{}
	args["tracer"] = "callTracer"
  blockNumber := fmt.Sprintf("%#x",MockHeight103)
	seiResObj := sendRequestGoodWithNamespace(t, "sei", "traceBlockByNumberExcludeTraceFail", blockNumber, args)
	result := seiResObj["result"].([]interface{})
	// sei_traceBlockByNumberExcludeTraceFail returns 1 trace, and removes the panic tx
	require.Equal(t, 1, len(result))
	ethResObj := sendRequestGoodWithNamespace(t, "debug", "traceBlockByNumber", blockNumber, args)
	// eth_traceBlockByNumber returns 2 traces, including the panic tx
	require.Equal(t, 2, len(ethResObj["result"].([]interface{})))
}

func TestTraceBlockByHashExcludeTraceFail(t *testing.T) {
	args := map[string]interface{}{}
	args["tracer"] = "callTracer"
	seiResObj := sendRequestGoodWithNamespace(t, "sei", "traceBlockByHashExcludeTraceFail", DebugTracePanicBlockHash, args)
	result := seiResObj["result"].([]interface{})
	// sei_traceBlockByHashExcludeTraceFail returns 1 trace, and removes the panic tx
	require.Equal(t, 1, len(result))
	ethResObj := sendRequestGoodWithNamespace(t, "debug", "traceBlockByHash", DebugTracePanicBlockHash, args)
	// eth_traceBlockByHash returns 2 traces, including the panic tx
	require.Equal(t, 2, len(ethResObj["result"].([]interface{})))
}
