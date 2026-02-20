package keeper_test

import (
	"testing"

	"github.com/ethereum/go-ethereum/common"
	ethtypes "github.com/ethereum/go-ethereum/core/types"
	testkeeper "github.com/sei-protocol/sei-chain/giga/deps/testutil/keeper"
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
