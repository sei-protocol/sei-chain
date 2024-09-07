package evmrpc_test

import (
	"errors"
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
	require.Equal(t, "0xae3f3", result)
}

func TestAccounts(t *testing.T) {
	homeDir := t.TempDir()
	api := evmrpc.NewInfoAPI(nil, nil, nil, nil, homeDir, 1024, evmrpc.ConnectionTypeHTTP)
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
	require.Equal(t, "0x174876e800", result)
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
		expectedGasUsed   float64
		expectedError     error
	}

	Ctx = Ctx.WithBlockHeight(1) // Simulate context with a specific block height

	testCases := []feeHistoryTestCase{
		{name: "Valid request by number", blockCount: 1, lastBlock: "0x8", rewardPercentiles: []interface{}{0.5}, expectedOldest: "0x1", expectedReward: "0x0", expectedBaseFee: "0x174876e800", expectedGasUsed: 0.5},
		{name: "Valid request by latest", blockCount: 1, lastBlock: "latest", rewardPercentiles: []interface{}{0.5}, expectedOldest: "0x1", expectedReward: "0x0", expectedBaseFee: "0x174876e800", expectedGasUsed: 0.5},
		{name: "Valid request by earliest", blockCount: 1, lastBlock: "earliest", rewardPercentiles: []interface{}{0.5}, expectedOldest: "0x1", expectedReward: "0x0", expectedBaseFee: "0x174876e800", expectedGasUsed: 0.5},
		{name: "Request on the same block", blockCount: 1, lastBlock: "0x1", rewardPercentiles: []interface{}{0.5}, expectedOldest: "0x1", expectedReward: "0x0", expectedBaseFee: "0x174876e800", expectedGasUsed: 0.5},
		{name: "Request on future block", blockCount: 1, lastBlock: "0x9", rewardPercentiles: []interface{}{0.5}, expectedOldest: "0x1", expectedReward: "0x0", expectedBaseFee: "0x174876e800", expectedGasUsed: 0.5},
		{name: "Block count truncates", blockCount: 1025, lastBlock: "latest", rewardPercentiles: []interface{}{25}, expectedOldest: "0x1", expectedReward: "0x0", expectedBaseFee: "0x174876e800", expectedGasUsed: 0.5},
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
				require.Equal(t, tc.expectedGasUsed, resObj["gasUsedRatio"].([]interface{})[0].(float64))
			}
		})
	}

	Ctx = Ctx.WithBlockHeight(8) // Reset context to a new block height
}

func TestCalculatePercentiles(t *testing.T) {
	// all empty
	result := evmrpc.CalculatePercentiles([]float64{}, []evmrpc.GasAndReward{}, 0)
	require.Equal(t, 0, len(result))

	// empty GasAndRewards
	result = evmrpc.CalculatePercentiles([]float64{1}, []evmrpc.GasAndReward{}, 0)
	require.Equal(t, 0, len(result))

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
	assert.Equal(t, "0x0", resObj["result"])
}
