package keeper_test

import (
	"testing"

	"github.com/ethereum/go-ethereum/common"
	ethtypes "github.com/ethereum/go-ethereum/core/types"
	"github.com/sei-protocol/sei-chain/app"
	abci "github.com/sei-protocol/sei-chain/sei-tendermint/abci/types"
	"github.com/sei-protocol/sei-chain/x/evm/derived"
	"github.com/sei-protocol/sei-chain/x/evm/types"
	"github.com/stretchr/testify/require"
)

func TestEndBlock_NoReceiptForNonceMismatch(t *testing.T) {
	a := app.Setup(t, false, false, false)
	k := a.EvmKeeper
	ctx := a.GetContextForDeliverTx([]byte{}).WithBlockHeight(8)

	msg := mockEVMTransactionMessage(t)
	etx, _ := msg.AsTransaction()
	txHash := etx.Hash()

	k.BeginBlock(ctx)
	k.SetMsgs([]*types.MsgEVMTransaction{msg})
	k.SetTxResults([]*abci.ExecTxResult{{Code: 1, Log: "nonce mismatch"}})
	// No SetNonceBumped call — simulates a tx where startingNonce != txNonce,
	// so the nonce bump callback was never registered/executed.
	k.EndBlock(ctx, 0, 0)

	_, err := k.GetTransientReceipt(ctx, txHash, 0)
	require.Error(t, err, "should not create a receipt when nonce was not bumped")
}

func TestEndBlock_ReceiptCreatedWhenNonceBumped(t *testing.T) {
	a := app.Setup(t, false, false, false)
	k := a.EvmKeeper
	ctx := a.GetContextForDeliverTx([]byte{}).WithBlockHeight(8)

	msg := mockEVMTransactionMessage(t)
	// The synthetic-receipt path uses msg.Derived.SenderEVMAddr to populate
	// the receipt's `From` (and `ContractAddress` for contract-creation txs).
	// In production this is set by the ante handler's preprocess step.
	sender := common.HexToAddress("0x1111111111111111111111111111111111111111")
	msg.Derived = &derived.Derived{SenderEVMAddr: sender}
	etx, _ := msg.AsTransaction()
	txHash := etx.Hash()

	k.BeginBlock(ctx)
	k.SetMsgs([]*types.MsgEVMTransaction{msg})
	k.SetTxResults([]*abci.ExecTxResult{{Code: 1, Log: "some ante error"}})
	// Simulate that the nonce bump callback ran (startingNonce == txNonce).
	k.SetNonceBumped(ctx.WithTxIndex(0))
	k.EndBlock(ctx, 0, 0)

	receipt, err := k.GetTransientReceipt(ctx, txHash, 0)
	require.NoError(t, err, "should create a receipt when nonce was bumped")
	require.Equal(t, txHash.Hex(), receipt.TxHashHex)
	require.Equal(t, "some ante error", receipt.VmError)
	require.Equal(t, uint64(8), receipt.BlockNumber)

	// Receipt-iff-nonce-bumped invariant: the sender paid gasLimit *
	// effectiveGasPrice in ante, so the receipt must reflect that (the closest
	// in-spec analog is OOG: GasUsed = gasLimit). Without these fields RPC
	// clients see GasUsed=0 / From=zero for a charged-and-included tx.
	require.Equal(t, uint32(ethtypes.ReceiptStatusFailed), receipt.Status)
	require.Equal(t, uint32(etx.Type()), receipt.TxType)
	require.Equal(t, etx.Gas(), receipt.GasUsed)
	require.Equal(t, sender.Hex(), receipt.From)
	require.NotZero(t, receipt.EffectiveGasPrice)
	if etx.To() != nil {
		require.Equal(t, etx.To().Hex(), receipt.To)
	}
}
