package evmrpc_test

import (
	"testing"

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
	resObj := sendRequestGoodWithNamespace(t, "debug", "traceBlockByHash", "0xBE17E0261E539CB7E9A91E123A6D794E0163D656FCF9B8EAC07823F7ED28512B", args)
	result := resObj["result"].([]interface{})[0].(map[string]interface{})["result"].(map[string]interface{})
	require.Equal(t, "0x5b4eba929f3811980f5ae0c5d04fa200f837df4e", result["from"])
	require.Equal(t, "0x55f0", result["gas"])
	require.Equal(t, "0x616263", result["input"])
	require.Equal(t, "0x0000000000000000000000000000000000010203", result["to"])
	require.Equal(t, "CALL", result["type"])
	require.Equal(t, "0x3e8", result["value"])

	// test prestateTracer
	args["tracer"] = "prestateTracer"
	resObj = sendRequestGoodWithNamespace(t, "debug", "traceBlockByHash", "0xBE17E0261E539CB7E9A91E123A6D794E0163D656FCF9B8EAC07823F7ED28512B", args)
	result = resObj["result"].([]interface{})[0].(map[string]interface{})["result"].(map[string]interface{})
	require.Equal(t, 3, len(result))
}
