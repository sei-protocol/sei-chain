package keeper_test

import (
	"math/big"
	"testing"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/core"
	ethtypes "github.com/ethereum/go-ethereum/core/types"
	testkeeper "github.com/sei-protocol/sei-chain/giga/deps/testutil/keeper"
	"github.com/sei-protocol/sei-chain/giga/deps/xevm/state"
	"github.com/sei-protocol/sei-chain/giga/deps/xevm/types"
	sdk "github.com/sei-protocol/sei-chain/sei-cosmos/types"
	"github.com/stretchr/testify/require"
)

func TestReceipt(t *testing.T) {
	k, ctx := testkeeper.MockEVMKeeper(t)
	txHash := common.HexToHash("0x0750333eac0be1203864220893d8080dd8a8fd7a2ed098dfd92a718c99d437f2")
	_, err := k.GetReceipt(ctx, txHash)
	require.NotNil(t, err)
	k.MockReceipt(ctx, txHash, &types.Receipt{TxHashHex: txHash.Hex()})
	k.AppendToEvmTxDeferredInfo(ctx, ethtypes.Bloom{}, common.Hash{1}, sdk.NewInt(1)) // make sure this isn't flushed into receipt store
	r, err := k.GetTransientReceipt(ctx, txHash, 0)
	require.Nil(t, err)
	require.Equal(t, txHash.Hex(), r.TxHashHex)
	_, err = k.GetTransientReceipt(ctx, common.Hash{1}, 0)
	require.Equal(t, "receipt not found", err.Error())
}

func TestDeleteTransientReceipt(t *testing.T) {
	k, ctx := testkeeper.MockEVMKeeper(t)
	txHash := common.HexToHash("0x1234567890abcdef1234567890abcdef1234567890abcdef1234567890abcdef")
	receipt := &types.Receipt{TxHashHex: txHash.Hex(), Status: 1}

	err := k.SetTransientReceipt(ctx, txHash, receipt)
	require.NoError(t, err)

	k.DeleteTransientReceipt(ctx, txHash, 0)

	receipt, err = k.GetTransientReceipt(ctx, txHash, 0)
	require.Nil(t, receipt)
	require.Equal(t, "receipt not found", err.Error())
}

// TestWriteReceiptStoresMsgGasPriceAsEffectiveGasPrice documents the contract
// between WriteReceipt and its caller for EIP-1559 dynamic-fee txs:
// receipt.EffectiveGasPrice is taken from msg.GasPrice verbatim, so the caller
// is responsible for setting msg.GasPrice = min(baseFee + tipCap, feeCap)
// (the actual effective gas price the chain charged).
//
// The bug this guards against: passing ethTx.GasPrice() into msg for a
// dynamic-fee tx returns GasFeeCap (i.e., maxFee) — the receipt would then
// report maxFee even when the tx actually paid baseFee+tip < maxFee, breaking
// EIP-1559 RPC semantics for clients (e.g. ethers, hardhat-ethers).
func TestWriteReceiptStoresMsgGasPriceAsEffectiveGasPrice(t *testing.T) {
	k, ctx := testkeeper.MockEVMKeeper(t)
	stateDB := state.NewDBImpl(ctx, k, false)
	txHash := common.HexToHash("0xdeadbeefdeadbeefdeadbeefdeadbeefdeadbeefdeadbeefdeadbeefdeadbeef")

	// Scenario: tip=1gwei, maxFee=10gwei, baseFee=1gwei →
	// effectiveGasPrice = baseFee + tip = 2gwei (additive branch).
	const (
		effectiveGasPrice uint64 = 2_000_000_000  // 2 gwei
		feeCap            uint64 = 10_000_000_000 // 10 gwei
		tipCap            uint64 = 1_000_000_000  // 1 gwei
		gasUsed           uint64 = 21_000
	)

	msg := &core.Message{
		Nonce:     0,
		GasLimit:  gasUsed,
		GasPrice:  new(big.Int).SetUint64(effectiveGasPrice),
		GasFeeCap: new(big.Int).SetUint64(feeCap),
		GasTipCap: new(big.Int).SetUint64(tipCap),
		To:        nil,
		Value:     big.NewInt(0),
		From:      common.HexToAddress("0x000000000000000000000000000000000000beef"),
	}

	r, err := k.WriteReceipt(ctx, stateDB, msg, uint32(ethtypes.DynamicFeeTxType), txHash, gasUsed, "")
	require.NoError(t, err)
	require.NotNil(t, r)
	require.Equal(t, effectiveGasPrice, r.EffectiveGasPrice,
		"receipt.EffectiveGasPrice must equal msg.GasPrice; if the caller passes "+
			"ethTx.GasPrice() for a dynamic-fee tx, the receipt would wrongly report GasFeeCap (%d) "+
			"instead of the EIP-1559 effective gas price (%d)", feeCap, effectiveGasPrice)
	require.Equal(t, uint32(ethtypes.ReceiptStatusSuccessful), r.Status)
	require.Equal(t, gasUsed, r.GasUsed)
}
