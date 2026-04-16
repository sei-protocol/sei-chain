// Tx decode benchmarks compare the legacy “decode twice then execute prep” shape vs the optimized
// “decode once then EVM finalize only” shape used when ProcessProposal passes pre-decoded txs into ProcessBlock.
//
// Run (from repo root):
//
//	go test ./app -bench=BenchmarkTxDecodePipeline -benchmem -count=5 -run=^$ 2>/dev/null | grep -E '^Benchmark.*ns/op'
//
// Use benchstat on saved outputs to compare ns/op and B/op between the two functions.
package app_test

import (
	"encoding/hex"
	"math/big"
	"testing"
	"time"

	"github.com/ethereum/go-ethereum/common"
	ethtypes "github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/crypto"
	"github.com/sei-protocol/sei-chain/app"
	"github.com/sei-protocol/sei-chain/sei-cosmos/crypto/keys/secp256k1"
	sdk "github.com/sei-protocol/sei-chain/sei-cosmos/types"
	banktypes "github.com/sei-protocol/sei-chain/sei-cosmos/x/bank/types"
	testkeeper "github.com/sei-protocol/sei-chain/testutil/keeper"
	"github.com/sei-protocol/sei-chain/x/evm/config"
	evmtypes "github.com/sei-protocol/sei-chain/x/evm/types"
	"github.com/sei-protocol/sei-chain/x/evm/types/ethtx"
)

// benchmarkEncodedTxBatch builds a block-sized batch of encoded txs (EVM + Cosmos),
// repeated from a small pattern so decode work dominates setup.
func benchmarkEncodedTxBatch(tw *app.TestWrapper, batchSize int) [][]byte {
	a := tw.App
	privKey := testkeeper.MockPrivateKey()
	key, _ := crypto.HexToECDSA(hex.EncodeToString(privKey.Bytes()))
	to := new(common.Address)
	copy(to[:], "0x1234567890abcdef1234567890abcdef12345678")
	txData := ethtypes.LegacyTx{
		Nonce:    1,
		GasPrice: big.NewInt(10),
		Gas:      1000,
		To:       to,
		Value:    big.NewInt(1000),
		Data:     []byte("abc"),
	}
	chainCfg := evmtypes.DefaultChainConfig()
	ethCfg := chainCfg.EthereumConfig(big.NewInt(config.DefaultChainID))
	signer := ethtypes.MakeSigner(ethCfg, big.NewInt(1), uint64(123))
	tx, err := ethtypes.SignTx(ethtypes.NewTx(&txData), signer, key)
	if err != nil {
		panic(err)
	}
	ethtxdata, _ := ethtx.NewTxDataFromTx(tx)
	msg, _ := evmtypes.NewMsgEVMTransaction(ethtxdata)
	txBuilder := a.GetTxConfig().NewTxBuilder()
	_ = txBuilder.SetMsgs(msg)
	evmtxbz, _ := a.GetTxConfig().TxEncoder()(txBuilder.GetTx())

	bankMsg := &banktypes.MsgSend{
		FromAddress: "",
		ToAddress:   "",
		Amount:      sdk.NewCoins(sdk.NewInt64Coin("usei", 2)),
	}
	bankTxBuilder := a.GetTxConfig().NewTxBuilder()
	_ = bankTxBuilder.SetMsgs(bankMsg)
	bankTxBuilder.SetGasLimit(200000)
	bankTxBuilder.SetFeeAmount(sdk.NewCoins(sdk.NewInt64Coin("usei", 20000)))
	banktxbz, _ := a.GetTxConfig().TxEncoder()(bankTxBuilder.GetTx())

	// Only valid encodings so benchmarks do not spam ERROR logs on every iteration.
	pattern := [][]byte{evmtxbz, banktxbz}
	txs := make([][]byte, 0, batchSize)
	for len(txs) < batchSize {
		for _, p := range pattern {
			if len(txs) >= batchSize {
				break
			}
			c := make([]byte, len(p))
			copy(c, p)
			txs = append(txs, c)
		}
	}
	return txs
}

// BenchmarkTxDecodePipeline_LegacyDuplicateDecodePlusFinalize approximates the pre-reuse hot path:
// a full byte decode (e.g. ProcessProposal gas accounting) plus ProcessBlock’s DecodeTransactionsConcurrently,
// which decodes the same bytes again and runs EVM preprocessing.
func BenchmarkTxDecodePipeline_LegacyDuplicateDecodePlusFinalize(b *testing.B) {
	const batchSize = 128
	tw := app.NewTestWrapper(b, time.Now().UTC(), secp256k1.GenPrivKey().PubKey(), false)
	txs := benchmarkEncodedTxBatch(tw, batchSize)
	ac := tw.App
	ctx := tw.Ctx
	b.ReportAllocs()
	b.ResetTimer()
	for b.Loop() {
		_ = ac.DecodeTxBytesConcurrently(txs)
		_ = ac.DecodeTransactionsConcurrently(ctx, txs)
	}
}

// BenchmarkTxDecodePipeline_OptimizedDecodeOncePlusFinalize matches the reuse path: one concurrent
// decode, then FinalizeDecodedTransactionsConcurrently only (no second protobuf decode).
func BenchmarkTxDecodePipeline_OptimizedDecodeOncePlusFinalize(b *testing.B) {
	const batchSize = 128
	tw := app.NewTestWrapper(b, time.Now().UTC(), secp256k1.GenPrivKey().PubKey(), false)
	txs := benchmarkEncodedTxBatch(tw, batchSize)
	ac := tw.App
	ctx := tw.Ctx
	b.ReportAllocs()
	b.ResetTimer()
	for b.Loop() {
		typed := ac.DecodeTxBytesConcurrently(txs)
		ac.FinalizeDecodedTransactionsConcurrently(ctx, typed)
	}
}
