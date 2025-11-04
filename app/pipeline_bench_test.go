package app

import (
	"context"
	"encoding/hex"
	"fmt"
	"math/big"
	"testing"
	"time"

	"github.com/cosmos/cosmos-sdk/crypto/keys/secp256k1"
	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/ethereum/go-ethereum/common"
	ethtypes "github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/crypto"
	testkeeper "github.com/sei-protocol/sei-chain/testutil/keeper"
	"github.com/sei-protocol/sei-chain/x/evm/config"
	evmtypes "github.com/sei-protocol/sei-chain/x/evm/types"
	"github.com/sei-protocol/sei-chain/x/evm/types/ethtx"
	"github.com/stretchr/testify/require"
	abci "github.com/tendermint/tendermint/abci/types"
)

// BenchmarkBlockProcessing compares legacy vs pipeline processing
func BenchmarkBlockProcessing(b *testing.B) {
	b.Run("Legacy", func(b *testing.B) {
		benchmarkBlockProcessingWithMode(b, false)
	})
	b.Run("Pipeline", func(b *testing.B) {
		benchmarkBlockProcessingWithMode(b, true)
	})
}

func benchmarkBlockProcessingWithMode(b *testing.B, usePipeline bool) {
	// Store original value
	originalValue := EnablePipelineProcessing
	defer func() {
		EnablePipelineProcessing = originalValue
	}()

	// Set pipeline mode
	EnablePipelineProcessing = usePipeline

	// Setup test environment
	tm := time.Now().UTC()
	valPub := secp256k1.GenPrivKey().PubKey()
	t := &testing.T{}
	testWrapper := NewTestWrapper(t, tm, valPub, false)
	
	// Create test account
	privKey := testkeeper.MockPrivateKey()
	key, err := crypto.HexToECDSA(hex.EncodeToString(privKey.Bytes()))
	require.NoError(b, err)
	testAcc := sdk.AccAddress(crypto.PubkeyToAddress(key.PublicKey).Bytes())
	testWrapper.FundAcc(testAcc, sdk.NewCoins(sdk.NewCoin("usei", sdk.NewInt(1000000000000000))))

	// Create multiple EVM transactions
	numTxs := 10
	txs := make([][]byte, numTxs)

	for i := 0; i < numTxs; i++ {
		txData := ethtypes.LegacyTx{
			Nonce:    uint64(i),
			GasPrice: big.NewInt(100000000000),
			Gas:      21000,
			To:       &common.Address{},
			Value:    big.NewInt(1000),
		}
		chainCfg := evmtypes.DefaultChainConfig()
		ethCfg := chainCfg.EthereumConfig(big.NewInt(config.DefaultChainID))
		signer := ethtypes.MakeSigner(ethCfg, big.NewInt(1), uint64(tm.Unix()))
		signedTx, err := ethtypes.SignTx(ethtypes.NewTx(&txData), signer, key)
		require.NoError(b, err)
		ethtxdata, err := ethtx.NewTxDataFromTx(signedTx)
		require.NoError(b, err)
		msg, err := evmtypes.NewMsgEVMTransaction(ethtxdata)
		require.NoError(b, err)
		txBuilder := testWrapper.App.GetTxConfig().NewTxBuilder()
		txBuilder.SetMsgs(msg)
		txBuilder.SetGasEstimate(21000)
		txbz, err := testWrapper.App.GetTxConfig().TxEncoder()(txBuilder.GetTx())
		require.NoError(b, err)
		txs[i] = txbz
	}

	// Initialize block height
	height := int64(1)
	testWrapper.Ctx = testWrapper.Ctx.WithBlockHeight(height)
	testWrapper.Ctx = testWrapper.Ctx.WithBlockTime(tm)

	// Reset timer before benchmarking
	b.ResetTimer()

	// Benchmark FinalizeBlock
	for i := 0; i < b.N; i++ {
		req := &abci.RequestFinalizeBlock{
			Height: height,
			Hash:   []byte(fmt.Sprintf("block-%d", height)),
			Time:   tm,
			Txs:    txs,
			DecidedLastCommit: abci.CommitInfo{
				Round: 0,
			},
		}

		resp, err := testWrapper.App.FinalizeBlocker(testWrapper.Ctx, req)
		require.NoError(b, err)
		require.NotNil(b, resp)

		// Commit state
		testWrapper.App.Commit(context.Background())
		height++
		testWrapper.Ctx = testWrapper.Ctx.WithBlockHeight(height)
		tm = tm.Add(6 * time.Second)
		testWrapper.Ctx = testWrapper.Ctx.WithBlockTime(tm)

		// Update nonces for next iteration
		for j := 0; j < numTxs; j++ {
			txData := ethtypes.LegacyTx{
				Nonce:    uint64(i*numTxs + j + numTxs),
				GasPrice: big.NewInt(100000000000),
				Gas:      21000,
				To:       &common.Address{},
				Value:    big.NewInt(1000),
			}
			chainCfg := evmtypes.DefaultChainConfig()
			ethCfg := chainCfg.EthereumConfig(big.NewInt(config.DefaultChainID))
			signer := ethtypes.MakeSigner(ethCfg, big.NewInt(height), uint64(tm.Unix()))
			signedTx, err := ethtypes.SignTx(ethtypes.NewTx(&txData), signer, key)
			require.NoError(b, err)
			ethtxdata, err := ethtx.NewTxDataFromTx(signedTx)
			require.NoError(b, err)
			msg, err := evmtypes.NewMsgEVMTransaction(ethtxdata)
			require.NoError(b, err)
			txBuilder := testWrapper.App.GetTxConfig().NewTxBuilder()
			txBuilder.SetMsgs(msg)
			txBuilder.SetGasEstimate(21000)
			txbz, err := testWrapper.App.GetTxConfig().TxEncoder()(txBuilder.GetTx())
			require.NoError(b, err)
			txs[j] = txbz
		}
	}
}

// BenchmarkBlockProcessingEmptyBlocks compares empty block processing
func BenchmarkBlockProcessingEmptyBlocks(b *testing.B) {
	b.Run("Legacy", func(b *testing.B) {
		benchmarkEmptyBlockProcessingWithMode(b, false)
	})
	b.Run("Pipeline", func(b *testing.B) {
		benchmarkEmptyBlockProcessingWithMode(b, true)
	})
}

func benchmarkEmptyBlockProcessingWithMode(b *testing.B, usePipeline bool) {
	// Store original value
	originalValue := EnablePipelineProcessing
	defer func() {
		EnablePipelineProcessing = originalValue
	}()

	// Set pipeline mode
	EnablePipelineProcessing = usePipeline

	// Setup test environment
	tm := time.Now().UTC()
	valPub := secp256k1.GenPrivKey().PubKey()
	t := &testing.T{}
	testWrapper := NewTestWrapper(t, tm, valPub, false)

	// Initialize block height
	height := int64(1)
	testWrapper.Ctx = testWrapper.Ctx.WithBlockHeight(height)
	testWrapper.Ctx = testWrapper.Ctx.WithBlockTime(tm)

	// Reset timer before benchmarking
	b.ResetTimer()

	// Benchmark FinalizeBlock with empty blocks
	for i := 0; i < b.N; i++ {
		req := &abci.RequestFinalizeBlock{
			Height: height,
			Hash:   []byte(fmt.Sprintf("block-%d", height)),
			Time:   tm,
			Txs:    [][]byte{},
			DecidedLastCommit: abci.CommitInfo{
				Round: 0,
			},
		}

		resp, err := testWrapper.App.FinalizeBlocker(testWrapper.Ctx, req)
		require.NoError(b, err)
		require.NotNil(b, resp)

		// Commit state
		testWrapper.App.Commit(context.Background())
		height++
		testWrapper.Ctx = testWrapper.Ctx.WithBlockHeight(height)
		tm = tm.Add(6 * time.Second)
		testWrapper.Ctx = testWrapper.Ctx.WithBlockTime(tm)
	}
}

// BenchmarkBlockProcessingManyTxs compares processing with many transactions
func BenchmarkBlockProcessingManyTxs(b *testing.B) {
	b.Run("Legacy", func(b *testing.B) {
		benchmarkManyTxsProcessingWithMode(b, false)
	})
	b.Run("Pipeline", func(b *testing.B) {
		benchmarkManyTxsProcessingWithMode(b, true)
	})
}

func benchmarkManyTxsProcessingWithMode(b *testing.B, usePipeline bool) {
	// Store original value
	originalValue := EnablePipelineProcessing
	defer func() {
		EnablePipelineProcessing = originalValue
	}()

	// Set pipeline mode
	EnablePipelineProcessing = usePipeline

	// Setup test environment
	tm := time.Now().UTC()
	valPub := secp256k1.GenPrivKey().PubKey()
	t := &testing.T{}
	testWrapper := NewTestWrapper(t, tm, valPub, false)
	
	// Create test account
	privKey := testkeeper.MockPrivateKey()
	key, err := crypto.HexToECDSA(hex.EncodeToString(privKey.Bytes()))
	require.NoError(b, err)
	testAcc := sdk.AccAddress(crypto.PubkeyToAddress(key.PublicKey).Bytes())
	testWrapper.FundAcc(testAcc, sdk.NewCoins(sdk.NewCoin("usei", sdk.NewInt(1000000000000000))))

	// Create many EVM transactions
	numTxs := 100
	txs := make([][]byte, numTxs)

	for i := 0; i < numTxs; i++ {
		txData := ethtypes.LegacyTx{
			Nonce:    uint64(i),
			GasPrice: big.NewInt(100000000000),
			Gas:      21000,
			To:       &common.Address{},
			Value:    big.NewInt(1000),
		}
		chainCfg := evmtypes.DefaultChainConfig()
		ethCfg := chainCfg.EthereumConfig(big.NewInt(config.DefaultChainID))
		signer := ethtypes.MakeSigner(ethCfg, big.NewInt(1), uint64(tm.Unix()))
		signedTx, err := ethtypes.SignTx(ethtypes.NewTx(&txData), signer, key)
		require.NoError(b, err)
		ethtxdata, err := ethtx.NewTxDataFromTx(signedTx)
		require.NoError(b, err)
		msg, err := evmtypes.NewMsgEVMTransaction(ethtxdata)
		require.NoError(b, err)
		txBuilder := testWrapper.App.GetTxConfig().NewTxBuilder()
		txBuilder.SetMsgs(msg)
		txBuilder.SetGasEstimate(21000)
		txbz, err := testWrapper.App.GetTxConfig().TxEncoder()(txBuilder.GetTx())
		require.NoError(b, err)
		txs[i] = txbz
	}

	// Initialize block height
	height := int64(1)
	testWrapper.Ctx = testWrapper.Ctx.WithBlockHeight(height)
	testWrapper.Ctx = testWrapper.Ctx.WithBlockTime(tm)

	// Reset timer before benchmarking
	b.ResetTimer()

	// Benchmark FinalizeBlock
	for i := 0; i < b.N; i++ {
		req := &abci.RequestFinalizeBlock{
			Height: height,
			Hash:   []byte(fmt.Sprintf("block-%d", height)),
			Time:   tm,
			Txs:    txs,
			DecidedLastCommit: abci.CommitInfo{
				Round: 0,
			},
		}

		resp, err := testWrapper.App.FinalizeBlocker(testWrapper.Ctx, req)
		require.NoError(b, err)
		require.NotNil(b, resp)

		// Commit state
		testWrapper.App.Commit(context.Background())
		height++
		testWrapper.Ctx = testWrapper.Ctx.WithBlockHeight(height)
		tm = tm.Add(6 * time.Second)
		testWrapper.Ctx = testWrapper.Ctx.WithBlockTime(tm)

		// Update nonces for next iteration
		for j := 0; j < numTxs; j++ {
			txData := ethtypes.LegacyTx{
				Nonce:    uint64(i*numTxs + j + numTxs),
				GasPrice: big.NewInt(100000000000),
				Gas:      21000,
				To:       &common.Address{},
				Value:    big.NewInt(1000),
			}
			chainCfg := evmtypes.DefaultChainConfig()
			ethCfg := chainCfg.EthereumConfig(big.NewInt(config.DefaultChainID))
			signer := ethtypes.MakeSigner(ethCfg, big.NewInt(height), uint64(tm.Unix()))
			signedTx, err := ethtypes.SignTx(ethtypes.NewTx(&txData), signer, key)
			require.NoError(b, err)
			ethtxdata, err := ethtx.NewTxDataFromTx(signedTx)
			require.NoError(b, err)
			msg, err := evmtypes.NewMsgEVMTransaction(ethtxdata)
			require.NoError(b, err)
			txBuilder := testWrapper.App.GetTxConfig().NewTxBuilder()
			txBuilder.SetMsgs(msg)
			txBuilder.SetGasEstimate(21000)
			txbz, err := testWrapper.App.GetTxConfig().TxEncoder()(txBuilder.GetTx())
			require.NoError(b, err)
			txs[j] = txbz
		}
	}
}

