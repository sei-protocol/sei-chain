package evmrpc_test

import (
	"math/big"
	"testing"

	"github.com/cosmos/cosmos-sdk/client"
	"github.com/cosmos/cosmos-sdk/client/config"
	"github.com/cosmos/cosmos-sdk/crypto/hd"
	"github.com/cosmos/cosmos-sdk/crypto/keyring"
	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/cosmos/go-bip39"
	"github.com/sei-protocol/sei-chain/evmrpc"
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
	api := evmrpc.NewInfoAPI(nil, nil, nil, nil, homeDir)
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
	resObj := sendRequestGood(t, "coinbase")
	result := resObj["result"].(string)
	require.Equal(t, "0x27f7b8b8b5a4e71e8e9aa671f4e4031e3773303f", result)
}

func TestGasPrice(t *testing.T) {
	resObj := sendRequestGood(t, "gasPrice")
	result := resObj["result"].(string)
	require.Equal(t, "0xa", result)
}

func TestFeeHistory(t *testing.T) {
	bodyByNumber := []interface{}{"0x1", "0x8", []interface{}{0.5}}
	bodyByLatest := []interface{}{"0x1", "latest", []interface{}{0.5}}
	bodyByEarliest := []interface{}{"0x1", "earliest", []interface{}{0.5}}
	bodyOld := []interface{}{"0x1", "0x1", []interface{}{0.5}}
	bodyFuture := []interface{}{"0x1", "0x9", []interface{}{0.5}}
	expectedOldest := []string{"0x8", "0x8", "0x1", "0x1", "0x8"}
	for i, body := range [][]interface{}{
		bodyByNumber, bodyByLatest, bodyByEarliest, bodyOld, bodyFuture,
	} {
		resObj := sendRequestGood(t, "feeHistory", body...)
		resObj = resObj["result"].(map[string]interface{})
		require.Equal(t, expectedOldest[i], resObj["oldestBlock"].(string))
		rewards := resObj["reward"].([]interface{})
		require.Equal(t, 1, len(rewards))
		reward := rewards[0].([]interface{})
		require.Equal(t, 1, len(reward))
		require.Equal(t, "0xa", reward[0].(string))
		baseFeePerGas := resObj["baseFeePerGas"].([]interface{})
		require.Equal(t, 1, len(baseFeePerGas))
		require.Equal(t, "0x0", baseFeePerGas[0].(string))
		gasUsedRatio := resObj["gasUsedRatio"].([]interface{})
		require.Equal(t, 1, len(gasUsedRatio))
		require.Equal(t, 0.5, gasUsedRatio[0].(float64))
	}

	// bad percentile
	outOfRangeBody1 := []interface{}{"0x1", "0x8", []interface{}{-1}}
	outOfRangeBody2 := []interface{}{"0x1", "0x8", []interface{}{101}}
	outOfOrderBody := []interface{}{"0x1", "0x8", []interface{}{99, 1}}
	for _, body := range [][]interface{}{outOfRangeBody1, outOfRangeBody2, outOfOrderBody} {
		resObj := sendRequestGood(t, "feeHistory", body...)
		errMap := resObj["error"].(map[string]interface{})
		require.Equal(t, "invalid reward percentiles: must be ascending and between 0 and 100", errMap["message"].(string))
	}
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
