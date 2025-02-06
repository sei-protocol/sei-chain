package keeper_test

import (
	"testing"
	"time"

	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/ethereum/go-ethereum/common"
	ethtypes "github.com/ethereum/go-ethereum/core/types"
	testkeeper "github.com/sei-protocol/sei-chain/testutil/keeper"
	"github.com/sei-protocol/sei-chain/x/evm/types"
	"github.com/stretchr/testify/require"
)

func TestReceipt(t *testing.T) {
	k := &testkeeper.EVMTestApp.EvmKeeper
	ctx := testkeeper.EVMTestApp.GetContextForDeliverTx([]byte{})
	txHash := common.HexToHash("0x0750333eac0be1203864220893d8080dd8a8fd7a2ed098dfd92a718c99d437f2")
	_, err := k.GetReceipt(ctx, txHash)
	require.NotNil(t, err)
	k.MockReceipt(ctx, txHash, &types.Receipt{TxHashHex: txHash.Hex()})
	k.AppendToEvmTxDeferredInfo(ctx, ethtypes.Bloom{}, common.Hash{1}, sdk.NewInt(1)) // make sure this isn't flushed into receipt store
	r, err := k.GetReceipt(ctx, txHash)
	require.Nil(t, err)
	require.Equal(t, txHash.Hex(), r.TxHashHex)
	_, err = k.GetReceipt(ctx, common.Hash{1})
	require.Equal(t, "not found", err.Error())
}

func TestGetReceiptWithRetry(t *testing.T) {
	k := &testkeeper.EVMTestApp.EvmKeeper
	ctx := testkeeper.EVMTestApp.GetContextForDeliverTx([]byte{})
	txHash := common.HexToHash("0x0750333eac0be1203864220893d8080dd8a8fd7a2ed098dfd92a718c99d437f2")

	// Test initial failure
	_, err := k.GetReceiptWithRetry(ctx, txHash, 3)
	require.NotNil(t, err)
	require.Equal(t, "not found", err.Error())

	// Test successful retry
	// Simulate async receipt creation after a delay
	go func() {
		time.Sleep(300 * time.Millisecond) // Wait for first retry to fail
		k.MockReceipt(ctx, txHash, &types.Receipt{TxHashHex: txHash.Hex()})
	}()

	// This should succeed after retry
	r, err := k.GetReceiptWithRetry(ctx, txHash, 3)
	require.Nil(t, err)
	require.Equal(t, txHash.Hex(), r.TxHashHex)

	// Test max retries exceeded
	nonExistentHash := common.Hash{1}
	_, err = k.GetReceiptWithRetry(ctx, nonExistentHash, 2)
	require.NotNil(t, err)
	require.Equal(t, "not found", err.Error())
}
