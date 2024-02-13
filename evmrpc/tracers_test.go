package evmrpc_test

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestTraceTransaction(t *testing.T) {
	hash := "0x1234567890123456789023456789012345678901234567890123456789000004"
	args := map[string]interface{}{}
	args["tracer"] = "callTracer"
	resObj := sendRequestGoodWithNamespace(t, "debug", "traceTransaction", hash, args)
	result := resObj["result"].(map[string]interface{})
	require.Equal(t, "0x5b4eba929f3811980f5ae0c5d04fa200f837df4e", result["from"])
	require.Equal(t, "0x55f0", result["gas"])
	require.Equal(t, "0x616263", result["input"])
	require.Equal(t, "0x0000000000000000000000000000000000010203", result["to"])
	require.Equal(t, "CALL", result["type"])
	require.Equal(t, "0x3e8", result["value"])
}
