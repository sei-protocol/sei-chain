package evmrpc_test

import (
	"fmt"
	"testing"

	"github.com/ethereum/go-ethereum/common"
	testkeeper "github.com/sei-protocol/sei-chain/testutil/keeper"
	"github.com/sei-protocol/sei-chain/x/evm/types"
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

func TestTraceBlockByNumberWithFailedTransactions(t *testing.T) {
	// Test that TraceBlockByNumber properly handles failed transactions
	// Since we can't access internal functions from evmrpc_test package,
	// we test the end-to-end behavior through the RPC interface

	// Create test transaction hashes for failed transactions
	failedTxHash1 := common.HexToHash("0x7777777777777777777777777777777777777777777777777777777777777777")
	failedTxHash2 := common.HexToHash("0x8888888888888888888888888888888888888888888888888888888888888888")

	// Mock receipts for failed transactions
	ctx := Ctx.WithBlockHeight(MockHeight103)

	// Failed transaction without existing error
	err := EVMKeeper.MockReceipt(ctx, failedTxHash1, &types.Receipt{
		BlockNumber:      uint64(MockHeight103),
		TransactionIndex: 0,
		TxHashHex:        failedTxHash1.Hex(),
		Status:           0, // Failed
	})
	require.NoError(t, err, "MockReceipt should not return error")

	// Failed transaction with existing error
	err = EVMKeeper.MockReceipt(ctx, failedTxHash2, &types.Receipt{
		BlockNumber:      uint64(MockHeight103),
		TransactionIndex: 1,
		TxHashHex:        failedTxHash2.Hex(),
		Status:           0, // Failed
	})
	require.NoError(t, err, "MockReceipt should not return error")

	// Test traceBlockByNumber with callTracer
	args := map[string]interface{}{
		"tracer": "callTracer",
	}

	// Call traceBlockByNumber - this should trigger error decoration logic
	resObj := sendRequestGoodWithNamespace(t, "debug", "traceBlockByNumber", fmt.Sprintf("0x%x", MockHeight103), args)

	// Verify we got a result without errors
	result, ok := resObj["result"]
	require.True(t, ok, "expected result to be present")
	require.NotNil(t, result, "result should not be nil")

	// Verify no RPC-level errors occurred
	_, hasError := resObj["error"]
	require.False(t, hasError, "should not have RPC errors")
}

func TestTraceBlockByHashWithFailedTransactions(t *testing.T) {
	// Test that TraceBlockByHash properly handles failed transactions

	// Create test transaction hashes for failed transactions
	failedTxHash3 := common.HexToHash("0x9999999999999999999999999999999999999999999999999999999999999999")

	// Mock receipts for failed transactions
	ctx := Ctx.WithBlockHeight(MockHeight8)

	// Failed transaction
	err := EVMKeeper.MockReceipt(ctx, failedTxHash3, &types.Receipt{
		BlockNumber:      uint64(MockHeight8),
		TransactionIndex: 0,
		TxHashHex:        failedTxHash3.Hex(),
		Status:           0, // Failed
	})
	require.NoError(t, err, "MockReceipt should not return error")

	// Test traceBlockByHash with callTracer
	args := map[string]interface{}{
		"tracer": "callTracer",
	}

	// Call traceBlockByHash - this should trigger error decoration logic
	resObj := sendRequestGoodWithNamespace(t, "debug", "traceBlockByHash", MultiTxBlockHash, args)

	// Verify we got a result without errors
	result, ok := resObj["result"]
	require.True(t, ok, "expected result to be present")
	require.NotNil(t, result, "result should not be nil")

	// Verify no RPC-level errors occurred
	_, hasError := resObj["error"]
	require.False(t, hasError, "should not have RPC errors")
}

func TestErrorDecorationIntegration(t *testing.T) {
	// Integration test for error decoration functionality
	// Tests the behavior through the RPC interface and verifies the system works
	
	// Create test transaction hashes for failed transactions
	failedTxHash1 := common.HexToHash("0x1111111111111111111111111111111111111111111111111111111111111111")
	failedTxHash2 := common.HexToHash("0x2222222222222222222222222222222222222222222222222222222222222222")
	
	// Mock receipts for failed transactions at a specific block height
	ctx := Ctx.WithBlockHeight(200) // Use a unique height to avoid conflicts
	
	// Failed transaction without existing error
	err := EVMKeeper.MockReceipt(ctx, failedTxHash1, &types.Receipt{
		BlockNumber:      200,
		TransactionIndex: 0,
		TxHashHex:        failedTxHash1.Hex(),
		Status:           0, // Failed
	})
	require.NoError(t, err, "MockReceipt should not return error")
	
	// Another failed transaction
	err = EVMKeeper.MockReceipt(ctx, failedTxHash2, &types.Receipt{
		BlockNumber:      200,
		TransactionIndex: 1,
		TxHashHex:        failedTxHash2.Hex(),
		Status:           0, // Failed
	})
	require.NoError(t, err, "MockReceipt should not return error")
	
	// Test that the system handles failed transactions properly
	// The error decoration should happen internally when tracing
	
	// Test traceBlockByNumber - this should trigger error decoration logic
	args := map[string]interface{}{
		"tracer": "callTracer",
	}
	
	resObj := sendRequestGoodWithNamespace(t, "debug", "traceBlockByNumber", "0xc8", args) // 0xc8 = 200
	
	// Verify we got a result without RPC errors
	result, ok := resObj["result"]
	require.True(t, ok, "expected result to be present")
	require.NotNil(t, result, "result should not be nil")
	
	// Verify no RPC-level errors occurred
	_, hasError := resObj["error"]
	require.False(t, hasError, "should not have RPC errors")
	
	// The actual error decoration testing is done internally
	// This test verifies the integration works without crashes
}
