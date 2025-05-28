package evmrpc_test

import (
	"fmt"
	"sync"
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

func TestTraceTransactionTimeout(t *testing.T) {
	// Force an small per‑call timeout so the method fails fast
	args := map[string]interface{}{
		"tracer":  "callTracer",
		"timeout": "1ns",
	}

	// Expect the RPC layer to return an error object.
	resObj := sendRequestBadWithNamespace(t,
		"debug",
		"traceTransaction",
		DebugTraceHashHex,
		args,
	)

	errObj, ok := resObj["error"].(map[string]interface{})
	require.True(t, ok, "expected traceTransaction to error out")
	require.Contains(t, errObj["message"].(string), "context deadline exceeded")
}

func TestTraceBlockByNumberLookbackLimit(t *testing.T) {
	resObj := sendRequestBadWithNamespace(
		t,
		"sei",
		"traceBlockByNumberExcludeTraceFail",
		"0x0",                    // genesis block
		map[string]interface{}{}, // empty TraceConfig
	)

	errObj, ok := resObj["error"].(map[string]interface{})
	require.True(t, ok, "expected look‑back guard to trigger")
	require.Contains(t, errObj["message"].(string), "beyond max lookback")
}

func TestTraceCallConcurrencyLimit(t *testing.T) {
	_, from := testkeeper.MockAddressPair()
	_, contractAddr := testkeeper.MockAddressPair()

	txArgs := map[string]interface{}{
		"from":    from.Hex(),
		"to":      contractAddr.Hex(),
		"chainId": fmt.Sprintf("%#x", EVMKeeper.ChainID(Ctx)),
	}

	var wg sync.WaitGroup
	wg.Add(2)

	start := time.Now()

	call := func() {
		defer wg.Done()
		// "latest" is enough – the contract code is fixed in the fixture chain.
		sendRequestGoodWithNamespace(t, "debug", "traceCall", txArgs, "latest")
	}

	go call()
	go call()

	wg.Wait()
	elapsed := time.Since(start)

	// A single traceCall normally finishes in ~250 ms on CI hardware.
	require.GreaterOrEqual(t, elapsed, 500*time.Millisecond,
		"concurrency semaphore should have forced the calls to run serially",
	)
}
