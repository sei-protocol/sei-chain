package keeper_test

import (
	"testing"

	"github.com/sei-protocol/sei-chain/app"
	abci "github.com/sei-protocol/sei-chain/sei-tendermint/abci/types"
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
}
