package evmrpc_test

import (
	"context"
	"encoding/hex"
	"fmt"
	"math/big"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/cosmos/cosmos-sdk/client"
	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/ethereum/go-ethereum/common/hexutil"
	"github.com/ethereum/go-ethereum/core"
	"github.com/ethereum/go-ethereum/export"
	"github.com/sei-protocol/sei-chain/app"
	"github.com/sei-protocol/sei-chain/evmrpc"
	"github.com/sei-protocol/sei-chain/example/contracts/simplestorage"
	testkeeper "github.com/sei-protocol/sei-chain/testutil/keeper"
	"github.com/sei-protocol/sei-chain/x/evm/types"
	"github.com/stretchr/testify/require"
)

func TestEstimateGas(t *testing.T) {
	Ctx = Ctx.WithBlockHeight(1)
	// transfer
	_, from := testkeeper.MockAddressPair()
	_, to := testkeeper.MockAddressPair()
	txArgs := map[string]interface{}{
		"from":    from.Hex(),
		"to":      to.Hex(),
		"value":   "0x10",
		"nonce":   "0x1",
		"chainId": fmt.Sprintf("%#x", EVMKeeper.ChainID(Ctx)),
	}
	amts := sdk.NewCoins(sdk.NewCoin(EVMKeeper.GetBaseDenom(Ctx), sdk.NewInt(20)))
	EVMKeeper.BankKeeper().MintCoins(Ctx, types.ModuleName, amts)
	EVMKeeper.BankKeeper().SendCoinsFromModuleToAccount(Ctx, types.ModuleName, sdk.AccAddress(from[:]), amts)
	resObj := sendRequestGood(t, "estimateGas", txArgs, nil, map[string]interface{}{})
	result := resObj["result"].(string)
	require.Equal(t, "0x5208", result) // 21000
	resObj = sendRequestGood(t, "estimateGas", txArgs, "latest", map[string]interface{}{})
	result = resObj["result"].(string)
	require.Equal(t, "0x5208", result) // 21000
	resObj = sendRequestGood(t, "estimateGas", txArgs, "0x123456", map[string]interface{}{})
	result = resObj["result"].(string)
	require.Equal(t, "0x5208", result) // 21000

	// contract call
	_, contractAddr := testkeeper.MockAddressPair()
	code, err := os.ReadFile("../example/contracts/simplestorage/SimpleStorage.bin")
	require.Nil(t, err)
	bz, err := hex.DecodeString(string(code))
	require.Nil(t, err)
	abi, err := simplestorage.SimplestorageMetaData.GetAbi()
	require.Nil(t, err)
	input, err := abi.Pack("set", big.NewInt(20))
	require.Nil(t, err)
	EVMKeeper.SetCode(Ctx, contractAddr, bz)
	txArgs = map[string]interface{}{
		"from":    from.Hex(),
		"to":      contractAddr.Hex(),
		"value":   "0x0",
		"nonce":   "0x2",
		"chainId": fmt.Sprintf("%#x", EVMKeeper.ChainID(Ctx)),
		"input":   fmt.Sprintf("%#x", input),
	}
	resObj = sendRequestGood(t, "estimateGas", txArgs, nil, map[string]interface{}{})
	result = resObj["result"].(string)
	require.Equal(t, "0x54ac", result) // 21497

	Ctx = Ctx.WithBlockHeight(8)
}

func TestEstimateGasAfterCalls(t *testing.T) {
	Ctx = Ctx.WithBlockHeight(1)
	// estimate get after set
	_, from := testkeeper.MockAddressPair()
	amts := sdk.NewCoins(sdk.NewCoin(EVMKeeper.GetBaseDenom(Ctx), sdk.NewInt(20)))
	EVMKeeper.BankKeeper().MintCoins(Ctx, types.ModuleName, amts)
	EVMKeeper.BankKeeper().SendCoinsFromModuleToAccount(Ctx, types.ModuleName, sdk.AccAddress(from[:]), amts)
	_, contractAddr := testkeeper.MockAddressPair()
	code, err := os.ReadFile("../example/contracts/simplestorage/SimpleStorage.bin")
	require.Nil(t, err)
	bz, err := hex.DecodeString(string(code))
	require.Nil(t, err)
	abi, err := simplestorage.SimplestorageMetaData.GetAbi()
	require.Nil(t, err)
	call, err := abi.Pack("set", big.NewInt(20))
	require.Nil(t, err)
	input, err := abi.Pack("get")
	require.Nil(t, err)
	EVMKeeper.SetCode(Ctx, contractAddr, bz)
	txArgs := map[string]interface{}{
		"from":    from.Hex(),
		"to":      contractAddr.Hex(),
		"value":   "0x0",
		"nonce":   "0x2",
		"chainId": fmt.Sprintf("%#x", EVMKeeper.ChainID(Ctx)),
		"input":   fmt.Sprintf("%#x", input),
	}
	callArgs := map[string]interface{}{
		"from":    from.Hex(),
		"to":      contractAddr.Hex(),
		"value":   "0x0",
		"nonce":   "0x2",
		"chainId": fmt.Sprintf("%#x", EVMKeeper.ChainID(Ctx)),
		"input":   fmt.Sprintf("%#x", call),
	}
	resObj := sendRequestGood(t, "estimateGasAfterCalls", txArgs, []interface{}{callArgs}, nil, map[string]interface{}{})
	result := resObj["result"].(string)
	require.Equal(t, "0x536d", result) // 21357 for get

	Ctx = Ctx.WithBlockHeight(8)
}

func TestCreateAccessList(t *testing.T) {
	Ctx = Ctx.WithBlockHeight(1)

	_, from := testkeeper.MockAddressPair()
	_, contractAddr := testkeeper.MockAddressPair()
	code, err := os.ReadFile("../example/contracts/simplestorage/SimpleStorage.bin")
	require.Nil(t, err)
	bz, err := hex.DecodeString(string(code))
	require.Nil(t, err)
	abi, err := simplestorage.SimplestorageMetaData.GetAbi()
	require.Nil(t, err)
	input, err := abi.Pack("set", big.NewInt(20))
	require.Nil(t, err)
	EVMKeeper.SetCode(Ctx, contractAddr, bz)
	txArgs := map[string]interface{}{
		"from":    from.Hex(),
		"to":      contractAddr.Hex(),
		"value":   "0x0",
		"nonce":   "0x1",
		"chainId": fmt.Sprintf("%#x", EVMKeeper.ChainID(Ctx)),
		"input":   fmt.Sprintf("%#x", input),
	}
	amts := sdk.NewCoins(sdk.NewCoin(EVMKeeper.GetBaseDenom(Ctx), sdk.NewInt(2000000)))
	EVMKeeper.BankKeeper().MintCoins(Ctx, types.ModuleName, amts)
	EVMKeeper.BankKeeper().SendCoinsFromModuleToAccount(Ctx, types.ModuleName, sdk.AccAddress(from[:]), amts)
	resObj := sendRequestGood(t, "createAccessList", txArgs, "latest")
	result := resObj["result"].(map[string]interface{})
	require.Equal(t, []interface{}{}, result["accessList"]) // the code uses MSTORE which does not trace access list

	resObj = sendRequestBad(t, "createAccessList", txArgs, "latest")
	result = resObj["error"].(map[string]interface{})
	require.Equal(t, "error block", result["message"])

	Ctx = Ctx.WithBlockHeight(8)
}

func TestCall(t *testing.T) {
	Ctx = Ctx.WithBlockHeight(1)

	_, from := testkeeper.MockAddressPair()
	_, contractAddr := testkeeper.MockAddressPair()
	code, err := os.ReadFile("../example/contracts/simplestorage/SimpleStorage.bin")
	require.Nil(t, err)
	bz, err := hex.DecodeString(string(code))
	require.Nil(t, err)
	abi, err := simplestorage.SimplestorageMetaData.GetAbi()
	require.Nil(t, err)
	input, err := abi.Pack("set", big.NewInt(20))
	require.Nil(t, err)
	EVMKeeper.SetCode(Ctx, contractAddr, bz)
	txArgs := map[string]interface{}{
		"from":    from.Hex(),
		"to":      contractAddr.Hex(),
		"value":   "0x0",
		"nonce":   "0x2",
		"chainId": fmt.Sprintf("%#x", EVMKeeper.ChainID(Ctx)),
		"input":   fmt.Sprintf("%#x", input),
	}
	resObj := sendRequestGood(t, "call", txArgs, nil, map[string]interface{}{}, map[string]interface{}{})
	result := resObj["result"].(string)
	require.Equal(t, "0x608060405234801561000f575f80fd5b506004361061003f575f3560e01c806360fe47b1146100435780636d4ce63c1461005f5780639c3674fc1461007d575b5f80fd5b61005d6004803603810190610058919061010a565b610087565b005b6100676100c7565b6040516100749190610144565b60405180910390f35b6100856100cf565b005b805f819055507f0de2d86113046b9e8bb6b785e96a6228f6803952bf53a40b68a36dce316218c1816040516100bc9190610144565b60405180910390a150565b5f8054905090565b5f80fd5b5f80fd5b5f819050919050565b6100e9816100d7565b81146100f3575f80fd5b50565b5f81359050610104816100e0565b92915050565b5f6020828403121561011f5761011e6100d3565b5b5f61012c848285016100f6565b91505092915050565b61013e816100d7565b82525050565b5f6020820190506101575f830184610135565b9291505056fea2646970667358221220bb55137839ea2afda11ab2d30ad07fee30bb9438caaa46e30ccd1053ed72439064736f6c63430008150033", result)

	Ctx = Ctx.WithBlockHeight(8)
}

func TestEthCallHighAmount(t *testing.T) {
	Ctx = Ctx.WithBlockHeight(1)
	_, from := testkeeper.MockAddressPair()
	_, to := testkeeper.MockAddressPair()
	txArgs := map[string]interface{}{
		"from":    from.Hex(),
		"to":      to.Hex(),
		"value":   "0x0",
		"nonce":   "0x2",
		"chainId": fmt.Sprintf("%#x", EVMKeeper.ChainID(Ctx)),
	}

	overrides := map[string]map[string]interface{}{
		from.Hex(): {"balance": "0xffffffffffffffffffffffffffffffffffffffffffffffffffffffffffffffff"},
	}
	resObj := sendRequestGood(t, "call", txArgs, "latest", overrides)
	errMap := resObj["error"].(map[string]interface{})
	result := errMap["message"]
	require.Equal(t, result, "error: balance override overflow")

	Ctx = Ctx.WithBlockHeight(8)
}

func TestNewRevertError(t *testing.T) {
	err := evmrpc.NewRevertError(&core.ExecutionResult{})
	require.NotNil(t, err)
	require.Equal(t, 3, err.ErrorCode())
	require.Equal(t, "0x", err.ErrorData())
}

func TestConvertBlockNumber(t *testing.T) {
	backend := evmrpc.NewBackend(func(i int64) sdk.Context {
		if i == evmrpc.LatestCtxHeight {
			return sdk.Context{}.WithBlockHeight(1000)
		}
		return sdk.Context{}
	}, nil, nil, &MockClient{}, nil, nil, nil)
	require.Equal(t, int64(10), backend.ConvertBlockNumber(10))
	require.Equal(t, int64(1), backend.ConvertBlockNumber(0))
	require.Equal(t, int64(1000), backend.ConvertBlockNumber(-2))
	require.Equal(t, int64(1000), backend.ConvertBlockNumber(-3))
	require.Equal(t, int64(1000), backend.ConvertBlockNumber(-4))
}

func TestPreV620UpgradeUsesBaseFeeNil(t *testing.T) {
	// Set up a test context with a height before v6.2.0 upgrade
	// For pacific-1 chain, we need to set a height that's before the v6.2.0 upgrade
	testHeight := int64(1000) // A height before v6.2.0 upgrade

	// Create a new test app to have control over the upgrade keeper
	testApp := app.Setup(false, false, false)
	testCtx := testApp.GetContextForDeliverTx([]byte{}).WithBlockHeight(testHeight)

	// Set the chain ID to "pacific-1" to trigger the upgrade check
	testCtx = testCtx.WithChainID("pacific-1")

	// Set the v6.2.0 upgrade height to a height higher than our test height
	// This simulates that the upgrade hasn't happened yet
	v620UpgradeHeight := int64(2000)
	testApp.UpgradeKeeper.SetDone(testCtx.WithBlockHeight(v620UpgradeHeight), "6.2.0")

	// Create a backend with our test context provider
	ctxProvider := func(height int64) sdk.Context {
		return testCtx.WithBlockHeight(height)
	}

	config := &evmrpc.SimulateConfig{
		GasCap:     10000000,
		EVMTimeout: time.Second * 30,
	}

	backend := evmrpc.NewBackend(
		ctxProvider,
		&testApp.EvmKeeper,
		func(int64) client.TxConfig { return TxConfig },
		&MockClient{},
		config,
		testApp.BaseApp,
		testApp.TracerAnteHandler,
	)

	// Test HeaderByNumber with a height before v6.2.0 upgrade
	header, err := backend.HeaderByNumber(context.Background(), 1000)
	require.NoError(t, err)
	require.NotNil(t, header)

	// For pacific-1 chain before v6.2.0 upgrade, base fee should be nil
	require.Nil(t, header.BaseFee, "Base fee should be nil for pacific-1 chain before v6.2.0 upgrade")

	// Test with a height after v6.2.0 upgrade
	headerAfterUpgrade, err := backend.HeaderByNumber(context.Background(), 2500)
	require.NoError(t, err)
	require.NotNil(t, headerAfterUpgrade)

	// For pacific-1 chain after v6.2.0 upgrade, base fee should not be nil
	require.NotNil(t, headerAfterUpgrade.BaseFee, "Base fee should not be nil for pacific-1 chain after v6.2.0 upgrade")

	// Test with a different chain ID (not pacific-1)
	testCtxDifferentChain := testCtx.WithChainID("test-chain")
	ctxProviderDifferentChain := func(height int64) sdk.Context {
		return testCtxDifferentChain.WithBlockHeight(height)
	}

	backendDifferentChain := evmrpc.NewBackend(
		ctxProviderDifferentChain,
		&testApp.EvmKeeper,
		func(int64) client.TxConfig { return TxConfig },
		&MockClient{},
		config,
		testApp.BaseApp,
		testApp.TracerAnteHandler,
	)

	headerDifferentChain, err := backendDifferentChain.HeaderByNumber(context.Background(), 1000)
	require.NoError(t, err)
	require.NotNil(t, headerDifferentChain)

	// For non-pacific-1 chains, base fee should not be nil regardless of upgrade status
	require.NotNil(t, headerDifferentChain.BaseFee, "Base fee should not be nil for non-pacific-1 chains")
}

func TestSimulationAPIRequestLimiter(t *testing.T) {
	// Test setup using a proper context similar to other tests
	testCtx := Ctx.WithBlockHeight(1)

	// Create a simulation API with a very small request limiter to test rate limiting
	ctxProvider := func(height int64) sdk.Context {
		if height == evmrpc.LatestCtxHeight {
			return testCtx.WithIsTracing(true)
		}
		return testCtx.WithBlockHeight(height).WithIsTracing(true)
	}

	// Create a config with a small concurrency limit for reliable testing
	config := &evmrpc.SimulateConfig{
		GasCap:                       1000000,
		EVMTimeout:                   5 * time.Second,
		MaxConcurrentSimulationCalls: 2, // Small limit to easily trigger rate limiting
	}

	// Use the existing test app from the global setup
	testApp := testkeeper.TestApp()

	// Create simulation API
	simAPI := evmrpc.NewSimulationAPI(
		ctxProvider,
		EVMKeeper,
		func(int64) client.TxConfig { return TxConfig },
		&MockClient{},
		config,
		testApp.BaseApp,
		testApp.TracerAnteHandler,
		evmrpc.ConnectionTypeHTTP,
	)

	// Setup test data - create addresses and fund account
	_, from := testkeeper.MockAddressPair()
	_, to := testkeeper.MockAddressPair()

	// Fund the account for actual transactions
	amts := sdk.NewCoins(sdk.NewCoin(EVMKeeper.GetBaseDenom(testCtx), sdk.NewInt(2000000)))
	EVMKeeper.BankKeeper().MintCoins(testCtx, types.ModuleName, amts)
	EVMKeeper.BankKeeper().SendCoinsFromModuleToAccount(testCtx, types.ModuleName, sdk.AccAddress(from[:]), amts)

	// Helper function to create uint64 pointer
	uint64Ptr := func(v uint64) *uint64 { return &v }

	// Convert to export.TransactionArgs for eth_call
	args := export.TransactionArgs{
		From:  &from,
		To:    &to,
		Value: (*hexutil.Big)(big.NewInt(16)),
		Nonce: (*hexutil.Uint64)(uint64Ptr(1)),
	}

	t.Run("TestEthCallRateLimiting", func(t *testing.T) {
		// Test eth_call rate limiting with concurrent requests
		numRequests := 10 // Much more than the limit of 2
		results := make(chan error, numRequests)

		// Start all requests concurrently to overwhelm the rate limiter
		for i := 0; i < numRequests; i++ {
			go func() {
				_, err := simAPI.Call(context.Background(), args, nil, nil, nil)
				results <- err
			}()
		}

		// Collect all results
		var errors []error
		for i := 0; i < numRequests; i++ {
			errors = append(errors, <-results)
		}

		// Count successful vs rejected requests
		successCount := 0
		rejectedCount := 0
		for _, err := range errors {
			if err == nil {
				successCount++
			} else if strings.Contains(err.Error(), "eth_call rejected due to rate limit: server busy") {
				rejectedCount++
			} else {
				t.Logf("Unexpected error: %v", err)
			}
		}

		// With only 2 concurrent slots and 10 requests, we should have rejections
		require.Greater(t, rejectedCount, 0, "Should have rejected requests due to rate limiting")
		require.Greater(t, successCount, 0, "Should have some successful requests")
		require.Equal(t, numRequests, successCount+rejectedCount, "All requests should be accounted for")

		t.Logf("eth_call rate limiting: %d successful, %d rejected out of %d total", successCount, rejectedCount, numRequests)
	})

	t.Run("TestEstimateGasRateLimiting", func(t *testing.T) {
		// Test eth_estimateGas rate limiting
		numRequests := 8
		results := make(chan error, numRequests)

		// Start all requests concurrently
		for i := 0; i < numRequests; i++ {
			go func() {
				_, err := simAPI.EstimateGas(context.Background(), args, nil, nil)
				results <- err
			}()
		}

		// Collect all results
		var errors []error
		for i := 0; i < numRequests; i++ {
			errors = append(errors, <-results)
		}

		// Count successful vs rejected requests
		successCount := 0
		rejectedCount := 0
		for _, err := range errors {
			if err == nil {
				successCount++
			} else if strings.Contains(err.Error(), "eth_estimateGas rejected due to rate limit: server busy") {
				rejectedCount++
			} else {
				t.Logf("Unexpected estimateGas error: %v", err)
			}
		}

		// Should have some rejections due to rate limiting
		require.Greater(t, rejectedCount, 0, "Should have rejected estimateGas requests due to rate limiting")
		require.Equal(t, numRequests, successCount+rejectedCount, "All estimateGas requests should be accounted for")

		t.Logf("eth_estimateGas rate limiting: %d successful, %d rejected out of %d total", successCount, rejectedCount, numRequests)
	})

	t.Run("TestEstimateGasAfterCallsRateLimiting", func(t *testing.T) {
		// Test eth_estimateGasAfterCalls rate limiting
		numRequests := 2
		results := make(chan error, numRequests)

		// Create a simple call to use as a precondition
		callArgs := export.TransactionArgs{
			From:  &from,
			To:    &to,
			Value: (*hexutil.Big)(big.NewInt(8)),
			Nonce: (*hexutil.Uint64)(uint64Ptr(0)),
		}

		// Start all requests concurrently
		for i := 0; i < numRequests; i++ {
			go func() {
				_, err := simAPI.EstimateGasAfterCalls(context.Background(), args, []export.TransactionArgs{callArgs}, nil, nil)
				results <- err
			}()
		}

		// Collect all results
		var errors []error
		for i := 0; i < numRequests; i++ {
			errors = append(errors, <-results)
		}

		// Count successful vs rejected requests
		successCount := 0
		rejectedCount := 0
		for _, err := range errors {
			if err == nil {
				successCount++
			} else if strings.Contains(err.Error(), "eth_estimateGasAfterCalls rejected due to rate limit: server busy") {
				rejectedCount++
			} else {
				t.Logf("Unexpected estimateGasAfterCalls error: %v", err)
			}
		}

		// Should have no rejections within the rate limiting
		require.Equal(t, rejectedCount, 0, "Should have no rejected estimateGasAfterCalls requests")
		require.Equal(t, numRequests, successCount+rejectedCount, "All estimateGasAfterCalls requests should be accounted for")

		t.Logf("eth_estimateGasAfterCalls rate limiting: %d successful, %d rejected out of %d total", successCount, rejectedCount, numRequests)
	})

	t.Run("TestSequentialRequestsAfterLoad", func(t *testing.T) {
		numRequests := 10
		results := make(chan error, numRequests)

		for i := 0; i < numRequests; i++ {
			go func() {
				_, err := simAPI.Call(context.Background(), args, nil, nil, nil)
				results <- err
			}()
		}

		// Wait for all concurrent requests to finish
		for i := 0; i < numRequests; i++ {
			<-results
		}

		// Give a small amount of time for any ongoing operations to complete
		time.Sleep(50 * time.Millisecond)

		// Now send sequential requests and ensure they succeed
		for i := 0; i < 3; i++ {
			_, err := simAPI.Call(context.Background(), args, nil, nil, nil)
			require.NoError(t, err, "Sequential request %d should succeed after rate limiter recovers", i+1)
		}

		t.Log("Sequential requests after load: all succeeded")
	})

	t.Run("TestDifferentMethodsShareSameLimiter", func(t *testing.T) {
		// Test that different simulation methods share the same rate limiter
		numCallRequests := 3
		numEstimateRequests := 3
		totalRequests := numCallRequests + numEstimateRequests

		results := make(chan error, totalRequests)

		// Start mixed requests concurrently to verify they share the same limiter
		for i := 0; i < numCallRequests; i++ {
			go func() {
				_, err := simAPI.Call(context.Background(), args, nil, nil, nil)
				results <- err
			}()
		}

		for i := 0; i < numEstimateRequests; i++ {
			go func() {
				_, err := simAPI.EstimateGas(context.Background(), args, nil, nil)
				results <- err
			}()
		}

		// Collect all results
		var errors []error
		for i := 0; i < totalRequests; i++ {
			errors = append(errors, <-results)
		}

		// Count results
		successCount := 0
		rejectedCount := 0
		for _, err := range errors {
			if err == nil {
				successCount++
			} else if strings.Contains(err.Error(), "rejected due to rate limit: server busy") {
				rejectedCount++
			}
		}

		// Since the rate limiter allows 2 concurrent requests total, we should see some rejections
		// when running 6 concurrent requests across different methods
		require.Greater(t, rejectedCount, 0, "Different methods should share the same rate limiter")
		require.Equal(t, totalRequests, successCount+rejectedCount, "All mixed method requests should be accounted for")

		t.Logf("Mixed methods rate limiting: %d successful, %d rejected out of %d total", successCount, rejectedCount, totalRequests)
	})

	t.Run("TestRateLimitErrorFormat", func(t *testing.T) {
		// Test the error message format by overwhelming the rate limiter
		numRequests := 5
		results := make(chan error, numRequests)

		// Start requests concurrently to trigger rate limiting
		for i := 0; i < numRequests; i++ {
			go func() {
				_, err := simAPI.Call(context.Background(), args, nil, nil, nil)
				results <- err
			}()
		}

		// Collect results and check error messages
		var rateLimitErrors []error
		for i := 0; i < numRequests; i++ {
			if err := <-results; err != nil && strings.Contains(err.Error(), "rejected due to rate limit") {
				rateLimitErrors = append(rateLimitErrors, err)
			}
		}

		// Should have at least one rate limit error
		require.Greater(t, len(rateLimitErrors), 0, "Should have at least one rate limit error")

		// Verify error message format
		for _, err := range rateLimitErrors {
			require.Contains(t, err.Error(), "eth_call rejected due to rate limit: server busy")
			require.Contains(t, err.Error(), "server busy")
		}

		t.Logf("Found %d rate limit errors with correct format", len(rateLimitErrors))
	})
}
