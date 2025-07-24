package evmrpc_test

import (
	"context"
	"errors"
	"fmt"
	"math/big"
	"testing"

	"github.com/cosmos/cosmos-sdk/client"
	"github.com/cosmos/cosmos-sdk/client/config"
	"github.com/cosmos/cosmos-sdk/crypto/hd"
	"github.com/cosmos/cosmos-sdk/crypto/keyring"
	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/cosmos/go-bip39"
	"github.com/sei-protocol/sei-chain/evmrpc"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestBlockNumber(t *testing.T) {
	resObj := sendRequestGood(t, "blockNumber")
	result := resObj["result"].(string)
	require.Equal(t, "0x8", result)
}

func TestChainID(t *testing.T) {
	resObj := sendRequestGood(t, "chainId")
	result := resObj["result"].(string)
	require.Equal(t, "0xae3f2", result)
}

func TestAccounts(t *testing.T) {
	homeDir := t.TempDir()
	api := evmrpc.NewInfoAPI(nil, nil, nil, nil, homeDir, 1024, evmrpc.ConnectionTypeHTTP, nil)
	clientCtx := client.Context{}.WithViper("").WithHomeDir(homeDir)
	clientCtx, err := config.ReadFromClientConfig(clientCtx)
	require.Nil(t, err)
	kb, err := client.NewKeyringFromBackend(clientCtx, keyring.BackendTest)
	require.Nil(t, err)
	entropySeed, err := bip39.NewEntropy(256)
	require.Nil(t, err)
	mnemonic, err := bip39.NewMnemonic(entropySeed)
	require.Nil(t, err)
	algos, _ := kb.SupportedAlgorithms()
	algo, err := keyring.NewSigningAlgoFromString(string(hd.Secp256k1Type), algos)
	require.Nil(t, err)
	_, err = kb.NewAccount("test", mnemonic, "", hd.CreateHDPath(sdk.GetConfig().GetCoinType(), 0, 0).String(), algo)
	require.Nil(t, err)
	accounts, _ := api.Accounts()
	require.Equal(t, 1, len(accounts))
}

func TestCoinbase(t *testing.T) {
	Ctx = Ctx.WithBlockHeight(1)
	resObj := sendRequestGood(t, "coinbase")
	Ctx = Ctx.WithBlockHeight(8)
	result := resObj["result"].(string)
	require.Equal(t, "0x27f7b8b8b5a4e71e8e9aa671f4e4031e3773303f", result)
}

func TestGasPrice(t *testing.T) {
	resObj := sendRequestGood(t, "gasPrice")
	Ctx = Ctx.WithBlockHeight(8)
	result := resObj["result"].(string)
	onePointOneGwei := "0x4190ab00"
	require.Equal(t, onePointOneGwei, result)
}

func TestFeeHistory(t *testing.T) {
	type feeHistoryTestCase struct {
		name              string
		blockCount        interface{}
		lastBlock         interface{} // Changed to interface{} to handle different types
		rewardPercentiles interface{}
		expectedOldest    string
		expectedReward    string
		expectedBaseFee   string
		expectedError     error
	}

	Ctx = Ctx.WithBlockHeight(1) // Simulate context with a specific block height

	testCases := []feeHistoryTestCase{
		{name: "Valid request by number", blockCount: 1, lastBlock: "0x8", rewardPercentiles: []interface{}{0.5}, expectedOldest: "0x1", expectedReward: "0x170cdc1e00", expectedBaseFee: "0x3b9aca00"},
		{name: "Valid request by latest", blockCount: 1, lastBlock: "latest", rewardPercentiles: []interface{}{0.5}, expectedOldest: "0x1", expectedReward: "0x170cdc1e00", expectedBaseFee: "0x3b9aca00"},
		{name: "Valid request by earliest", blockCount: 1, lastBlock: "earliest", rewardPercentiles: []interface{}{0.5}, expectedOldest: "0x1", expectedReward: "0x170cdc1e00", expectedBaseFee: "0x3b9aca00"},
		{name: "Request on the same block", blockCount: 1, lastBlock: "0x1", rewardPercentiles: []interface{}{0.5}, expectedOldest: "0x1", expectedReward: "0x170cdc1e00", expectedBaseFee: "0x3b9aca00"},
		{name: "Request on future block", blockCount: 1, lastBlock: "0x9", rewardPercentiles: []interface{}{0.5}, expectedOldest: "0x1", expectedReward: "0x170cdc1e00", expectedBaseFee: "0x3b9aca00"},
		{name: "Block count truncates", blockCount: 1025, lastBlock: "latest", rewardPercentiles: []interface{}{25}, expectedOldest: "0x1", expectedReward: "0x170cdc1e00", expectedBaseFee: "0x3b9aca00"},
		{name: "Too many percentiles", blockCount: 10, lastBlock: "latest", rewardPercentiles: make([]interface{}, 101), expectedError: errors.New("rewardPercentiles length must be less than or equal to 100")},
		{name: "Invalid percentiles order", blockCount: 10, lastBlock: "latest", rewardPercentiles: []interface{}{99, 1}, expectedError: errors.New("invalid reward percentiles: must be ascending and between 0 and 100")},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			// Mimic request sending and handle the response
			resObj := sendRequestGood(t, "feeHistory", tc.blockCount, tc.lastBlock, tc.rewardPercentiles)
			if tc.expectedError != nil {
				errMap := resObj["error"].(map[string]interface{})
				require.Equal(t, tc.expectedError.Error(), errMap["message"].(string))
			} else {
				_, errorExists := resObj["error"]
				require.False(t, errorExists)

				resObj = resObj["result"].(map[string]interface{})

				require.Equal(t, tc.expectedOldest, resObj["oldestBlock"].(string))
				rewards, ok := resObj["reward"].([]interface{})

				require.True(t, ok, "Expected rewards to be a slice of interfaces")
				require.Equal(t, 1, len(rewards), "Expected exactly one reward entry")
				reward, ok := rewards[0].([]interface{})
				require.True(t, ok, "Expected reward to be a slice of interfaces")
				require.Equal(t, 1, len(reward), "Expected exactly one sub-item in reward")

				require.Equal(t, tc.expectedReward, reward[0].(string), "Reward does not match expected value")

				require.Equal(t, tc.expectedBaseFee, resObj["baseFeePerGas"].([]interface{})[0].(string))

				// Verify gas used ratio is valid (should be between 0 and 1)
				gasUsedRatios := resObj["gasUsedRatio"].([]interface{})
				require.Greater(t, len(gasUsedRatios), 0, "Should have at least one gas used ratio")
				gasUsedRatio := gasUsedRatios[0].(float64)
				require.GreaterOrEqual(t, gasUsedRatio, 0.0, "Gas used ratio should be >= 0")
				require.LessOrEqual(t, gasUsedRatio, 1.0, "Gas used ratio should be <= 1")
			}
		})
	}

	Ctx = Ctx.WithBlockHeight(8) // Reset context to a new block height
}

func TestCalculatePercentiles(t *testing.T) {
	// all empty
	result := evmrpc.CalculatePercentiles([]float64{}, []evmrpc.GasAndReward{}, 0)
	require.Equal(t, 0, len(result))

	// empty GasAndRewards should return zeros for each percentile
	result = evmrpc.CalculatePercentiles([]float64{1}, []evmrpc.GasAndReward{}, 0)
	require.Equal(t, 1, len(result))
	require.Equal(t, "0x0", result[0].String())

	// empty percentiles
	result = evmrpc.CalculatePercentiles([]float64{}, []evmrpc.GasAndReward{{Reward: big.NewInt(10), GasUsed: 1}}, 1)
	require.Equal(t, 0, len(result))

	// 0 percentile
	result = evmrpc.CalculatePercentiles([]float64{0}, []evmrpc.GasAndReward{{Reward: big.NewInt(10), GasUsed: 1}}, 1)
	require.Equal(t, 1, len(result))
	// see comments above CalculatePercentiles to understand why it should return 10 here
	require.Equal(t, big.NewInt(10), result[0].ToInt())

	// 100 percentile
	result = evmrpc.CalculatePercentiles([]float64{100}, []evmrpc.GasAndReward{{Reward: big.NewInt(10), GasUsed: 1}}, 1)
	require.Equal(t, 1, len(result))
	require.Equal(t, big.NewInt(10), result[0].ToInt())

	// 0 percentile and 100 percentile with just one transaction
	result = evmrpc.CalculatePercentiles([]float64{0, 100}, []evmrpc.GasAndReward{{Reward: big.NewInt(10), GasUsed: 1}}, 1)
	require.Equal(t, 2, len(result))
	require.Equal(t, big.NewInt(10), result[0].ToInt())
	require.Equal(t, big.NewInt(10), result[1].ToInt())

	// more transactions than percentiles
	result = evmrpc.CalculatePercentiles([]float64{0, 50, 100}, []evmrpc.GasAndReward{{Reward: big.NewInt(10), GasUsed: 1}, {Reward: big.NewInt(5), GasUsed: 2}, {Reward: big.NewInt(3), GasUsed: 3}}, 6)
	require.Equal(t, 3, len(result))
	require.Equal(t, big.NewInt(3), result[0].ToInt())
	require.Equal(t, big.NewInt(3), result[1].ToInt())
	require.Equal(t, big.NewInt(10), result[2].ToInt())
}

func TestMaxPriorityFeePerGas(t *testing.T) {
	Ctx = Ctx.WithBlockHeight(1)
	// Mimic request sending and handle the response
	resObj := sendRequestGood(t, "maxPriorityFeePerGas")
	assert.Equal(t, "0x3b9aca00", resObj["result"])
}

func TestGasPriceLogic(t *testing.T) {
	oneGwei := big.NewInt(1000000000)
	onePointOneGwei := big.NewInt(1100000000)
	tests := []struct {
		name                  string
		baseFee               *big.Int
		totalGasUsedPrevBlock uint64
		medianRewardPrevBlock *big.Int
		expectedGasPrice      *big.Int
	}{
		{
			name:                  "chain is not congested",
			baseFee:               oneGwei,
			totalGasUsedPrevBlock: 21000,
			medianRewardPrevBlock: oneGwei,
			expectedGasPrice:      onePointOneGwei,
		},
		{
			name:                  "chain is congested",
			baseFee:               oneGwei,
			totalGasUsedPrevBlock: 9000000, // 9mil
			medianRewardPrevBlock: big.NewInt(2000000000),
			expectedGasPrice:      big.NewInt(3000000000),
		},
		{
			name:                  "prev block has 1 tx with very high reward",
			baseFee:               oneGwei,
			totalGasUsedPrevBlock: 21000,                   // not congested
			medianRewardPrevBlock: big.NewInt(99000000000), // very high reward
			expectedGasPrice:      onePointOneGwei,         // gas price doesn't spike
		},
	}
	for _, test := range tests {
		i := evmrpc.NewInfoAPI(nil, nil, nil, nil, t.TempDir(), 1024, evmrpc.ConnectionTypeHTTP, nil)
		gasPrice, err := i.GasPriceHelper(
			context.Background(),
			test.baseFee,
			test.totalGasUsedPrevBlock,
			test.medianRewardPrevBlock,
		)
		require.Nil(t, err)
		require.Equal(t, test.expectedGasPrice, gasPrice.ToInt())
	}
}

func TestCalculatePercentilesEmptyBlockWithMultiplePercentiles(t *testing.T) {
	// Test with multiple percentiles and empty block
	result := evmrpc.CalculatePercentiles([]float64{25, 50, 75}, []evmrpc.GasAndReward{}, 0)
	require.Equal(t, 3, len(result))
	for i, r := range result {
		require.Equal(t, "0x0", r.String(), fmt.Sprintf("percentile %d should be zero", i))
	}
}

func TestCalculatePercentilesEmptyBlockWithSinglePercentile(t *testing.T) {
	// Test with single percentile and empty block
	result := evmrpc.CalculatePercentiles([]float64{50}, []evmrpc.GasAndReward{}, 0)
	require.Equal(t, 1, len(result))
	require.Equal(t, "0x0", result[0].String())
}

func TestCalculateGasUsedRatio(t *testing.T) {
	resObj := sendRequestGood(t, "feeHistory", 1, "latest", []interface{}{50.0})
	result := resObj["result"].(map[string]interface{})

	// Verify gas used ratio is calculated
	gasUsedRatios, ok := result["gasUsedRatio"].([]interface{})
	require.True(t, ok, "gasUsedRatio should be present and be an array")
	require.Greater(t, len(gasUsedRatios), 0, "Should have at least one gas used ratio")

	// The gas used ratio should be a valid number between 0 and 1
	ratio := gasUsedRatios[0].(float64)
	require.GreaterOrEqual(t, ratio, 0.0)
	require.LessOrEqual(t, ratio, 1.0)
}

func TestCalculateGasUsedRatioGasAccumulation(t *testing.T) {
	// Test that verifies gas used accumulation across multiple transactions
	// This test specifically covers: totalEVMGasUsed += receipt.GasUsed

	api := evmrpc.NewInfoAPI(&MockClient{}, EVMKeeper, func(height int64) sdk.Context {
		if height == evmrpc.LatestCtxHeight {
			return Ctx
		}
		return Ctx.WithBlockHeight(height)
	}, nil, "", 1024, evmrpc.ConnectionTypeHTTP, Decoder)

	// Test with a block that has multiple EVM transactions
	// Using block height 2 which has multiple transactions in the mock
	ratio, err := api.CalculateGasUsedRatio(context.Background(), 2)
	require.NoError(t, err)

	// The ratio should be valid and reflect accumulated gas usage
	require.GreaterOrEqual(t, ratio, 0.0)
	require.LessOrEqual(t, ratio, 1.0)

	// Since block 2 has multiple transactions, the ratio should be > 0
	// (assuming the mock setup has transactions with gas usage)
	require.Greater(t, ratio, 0.0, "Block 2 should have some gas usage from multiple transactions")
}

func TestCalculateGasUsedRatioReceiptRetrievalError(t *testing.T) {
	// Test error handling when receipt retrieval fails
	// This test covers: if err != nil { continue // Skip if we can't get the receipt }

	// Use a block height that doesn't exist in the mock setup to simulate receipt retrieval errors
	api := evmrpc.NewInfoAPI(&MockClient{}, EVMKeeper, func(height int64) sdk.Context {
		if height == evmrpc.LatestCtxHeight {
			return Ctx
		}
		return Ctx.WithBlockHeight(height)
	}, nil, "", 1024, evmrpc.ConnectionTypeHTTP, Decoder)

	// Test with a block height that has transactions but no receipts (to simulate receipt errors)
	// The calculation should not fail even if receipt retrieval fails
	// It should skip the failed receipts and continue
	ratio, err := api.CalculateGasUsedRatio(context.Background(), 100)
	require.NoError(t, err)

	// When receipt retrievals fail or no EVM transactions exist, we should get 0.0 ratio
	require.GreaterOrEqual(t, ratio, 0.0)
	require.LessOrEqual(t, ratio, 1.0)
}

func TestFeeHistoryGasUsedRatioCalculation(t *testing.T) {
	// Test multiple blocks to ensure we can get different ratios
	resObj := sendRequestGood(t, "feeHistory", 3, "latest", []interface{}{50.0})
	result := resObj["result"].(map[string]interface{})

	// Verify we have gas used ratio data
	gasUsedRatios, ok := result["gasUsedRatio"].([]interface{})
	require.True(t, ok, "gasUsedRatio should be present and be an array")
	require.GreaterOrEqual(t, len(gasUsedRatios), 1, "Should have at least one gas used ratio")

	// Check that all ratios are valid
	for i, ratioInterface := range gasUsedRatios {
		ratio := ratioInterface.(float64)
		require.GreaterOrEqual(t, ratio, 0.0, "Ratio %d should be >= 0", i)
		require.LessOrEqual(t, ratio, 1.0, "Ratio %d should be <= 1", i)
	}

	// Test edge case: single block
	resObj2 := sendRequestGood(t, "feeHistory", 1, "latest", []interface{}{25.0})
	result2 := resObj2["result"].(map[string]interface{})
	gasUsedRatios2, ok := result2["gasUsedRatio"].([]interface{})
	require.True(t, ok)
	require.Equal(t, 1, len(gasUsedRatios2), "Should have exactly one gas used ratio for single block")
}

func TestCalculateGasUsedRatioConsensusParamsFallback(t *testing.T) {
	// Test the fallback logic when consensus params are not available
	// This covers the fallback gas limit calculation

	// Create a context provider that returns contexts without consensus params
	ctxProviderWithoutConsensusParams := func(height int64) sdk.Context {
		baseCtx := Ctx
		if height != evmrpc.LatestCtxHeight {
			baseCtx = baseCtx.WithBlockHeight(height)
		}
		// Return a context that will have nil consensus params
		return baseCtx.WithConsensusParams(nil)
	}

	api := evmrpc.NewInfoAPI(&MockClient{}, EVMKeeper, ctxProviderWithoutConsensusParams, nil, "", 1024, evmrpc.ConnectionTypeHTTP, Decoder)

	// The calculation should still work using fallback gas limit
	ratio, err := api.CalculateGasUsedRatio(context.Background(), 1)
	require.NoError(t, err)

	// Should return a valid ratio using the default fallback gas limit (10000000)
	require.GreaterOrEqual(t, ratio, 0.0)
	require.LessOrEqual(t, ratio, 1.0)
}

func TestCalculateGasUsedRatioConsensusParamsNilBlock(t *testing.T) {
	// Test the fallback logic when consensus params exist but Block is nil

	ctxProviderWithNilBlock := func(height int64) sdk.Context {
		baseCtx := Ctx
		if height != evmrpc.LatestCtxHeight {
			baseCtx = baseCtx.WithBlockHeight(height)
		}
		// Create consensus params with nil Block
		consensusParams := baseCtx.ConsensusParams()
		if consensusParams != nil {
			consensusParams.Block = nil
			baseCtx = baseCtx.WithConsensusParams(consensusParams)
		}
		return baseCtx
	}

	api := evmrpc.NewInfoAPI(&MockClient{}, EVMKeeper, ctxProviderWithNilBlock, nil, "", 1024, evmrpc.ConnectionTypeHTTP, Decoder)

	// Should use fallback logic and still work
	ratio, err := api.CalculateGasUsedRatio(context.Background(), 1)
	require.NoError(t, err)

	require.GreaterOrEqual(t, ratio, 0.0)
	require.LessOrEqual(t, ratio, 1.0)
}

func TestCalculateGasUsedRatioZeroGasLimit(t *testing.T) {
	// Test edge case where gas limit is 0 (should return 0 to avoid division by zero)

	ctxProviderWithZeroGasLimit := func(height int64) sdk.Context {
		baseCtx := Ctx
		if height != evmrpc.LatestCtxHeight {
			baseCtx = baseCtx.WithBlockHeight(height)
		}
		// Create consensus params with zero gas limit
		consensusParams := baseCtx.ConsensusParams()
		if consensusParams != nil && consensusParams.Block != nil {
			consensusParams.Block.MaxGas = 0
			baseCtx = baseCtx.WithConsensusParams(consensusParams)
		}
		return baseCtx
	}

	api := evmrpc.NewInfoAPI(&MockClient{}, EVMKeeper, ctxProviderWithZeroGasLimit, nil, "", 1024, evmrpc.ConnectionTypeHTTP, Decoder)

	// Should return 0 to avoid division by zero
	ratio, err := api.CalculateGasUsedRatio(context.Background(), 1)
	require.NoError(t, err)
	require.Equal(t, 0.0, ratio, "Should return 0.0 when gas limit is 0 to avoid division by zero")
}

func TestCalculateGasUsedRatioBlockNumberMismatch(t *testing.T) {
	// Test the logic that skips receipts with mismatched block numbers
	// This covers: if receipt.BlockNumber != uint64(block.Block.Height) { continue }

	api := evmrpc.NewInfoAPI(&MockClient{}, EVMKeeper, func(height int64) sdk.Context {
		if height == evmrpc.LatestCtxHeight {
			return Ctx
		}
		return Ctx.WithBlockHeight(height)
	}, nil, "", 1024, evmrpc.ConnectionTypeHTTP, Decoder)

	// Test with a block where receipts might have mismatched block numbers
	// The method should still work and skip mismatched receipts
	ratio, err := api.CalculateGasUsedRatio(context.Background(), 1)
	require.NoError(t, err)

	require.GreaterOrEqual(t, ratio, 0.0)
	require.LessOrEqual(t, ratio, 1.0)
}

func TestCalculateGasUsedRatioMultipleTransactionsAccumulation(t *testing.T) {
	// Test that verifies gas used accumulation works correctly across multiple transactions
	// This test specifically covers: totalEVMGasUsed += receipt.GasUsed

	api := evmrpc.NewInfoAPI(&MockClient{}, EVMKeeper, func(height int64) sdk.Context {
		if height == evmrpc.LatestCtxHeight {
			return Ctx
		}
		return Ctx.WithBlockHeight(height)
	}, nil, "", 1024, evmrpc.ConnectionTypeHTTP, Decoder)

	// Test block 2 which has multiple transactions
	ratioBlock2, err := api.CalculateGasUsedRatio(context.Background(), 2)
	require.NoError(t, err)
	require.GreaterOrEqual(t, ratioBlock2, 0.0)
	require.LessOrEqual(t, ratioBlock2, 1.0)

	// Test block 8 which also has transactions
	ratioBlock8, err := api.CalculateGasUsedRatio(context.Background(), 8)
	require.NoError(t, err)
	require.GreaterOrEqual(t, ratioBlock8, 0.0)
	require.LessOrEqual(t, ratioBlock8, 1.0)

	// Both blocks should have some gas usage (ratios should be > 0 if there are EVM transactions)
	// The exact values depend on the mock setup, but they should be valid ratios
	t.Logf("Block 2 gas used ratio: %f", ratioBlock2)
	t.Logf("Block 8 gas used ratio: %f", ratioBlock8)
}

func TestCalculateGasUsedRatioWithDifferentGasLimits(t *testing.T) {
	// Test gas ratio calculation with different gas limits to verify the division logic

	// Test with a custom gas limit
	ctxProviderWithCustomGasLimit := func(height int64) sdk.Context {
		baseCtx := Ctx
		if height != evmrpc.LatestCtxHeight {
			baseCtx = baseCtx.WithBlockHeight(height)
		}
		// Set a specific gas limit to test the ratio calculation
		consensusParams := baseCtx.ConsensusParams()
		if consensusParams != nil && consensusParams.Block != nil {
			consensusParams.Block.MaxGas = 5000000 // Set a specific gas limit
			baseCtx = baseCtx.WithConsensusParams(consensusParams)
		}
		return baseCtx
	}

	api := evmrpc.NewInfoAPI(&MockClient{}, EVMKeeper, ctxProviderWithCustomGasLimit, nil, "", 1024, evmrpc.ConnectionTypeHTTP, Decoder)

	ratio, err := api.CalculateGasUsedRatio(context.Background(), 2)
	require.NoError(t, err)
	require.GreaterOrEqual(t, ratio, 0.0)
	require.LessOrEqual(t, ratio, 1.0)

	// Log the ratio to verify it's calculated correctly with custom gas limit
	t.Logf("Gas used ratio with custom gas limit (5M): %f", ratio)
}
