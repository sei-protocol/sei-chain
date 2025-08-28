package keeper_test

import (
	"testing"

	"github.com/cosmos/cosmos-sdk/store/prefix"
	sdk "github.com/cosmos/cosmos-sdk/types"
	testkeeper "github.com/sei-protocol/sei-chain/testutil/keeper"
	"github.com/sei-protocol/sei-chain/x/evm/keeper"
	"github.com/sei-protocol/sei-chain/x/evm/types"
	"github.com/stretchr/testify/require"
)

func TestRemoveFirstNTxHashes(t *testing.T) {
	k := &testkeeper.EVMTestApp.EvmKeeper
	ctx := testkeeper.EVMTestApp.GetContextForDeliverTx([]byte{})

	for i := byte(1); i <= 101; i++ {
		setTxHash(ctx, k, i, 102-i)
	}

	require.Equal(t, 101, getTxHashCount(ctx, k))
	k.RemoveFirstNTxHashes(ctx, keeper.DefaultTxHashesToRemove)
	require.Equal(t, 1, getTxHashCount(ctx, k))
	k.RemoveFirstNTxHashes(ctx, keeper.DefaultTxHashesToRemove)
	require.Equal(t, 0, getTxHashCount(ctx, k))
}

func setTxHash(ctx sdk.Context, k *keeper.Keeper, key byte, value byte) {
	store := prefix.NewStore(ctx.KVStore(k.GetStoreKey()), types.TxHashesPrefix)
	store.Set([]byte{key}, []byte{value})
}

func getTxHashCount(ctx sdk.Context, k *keeper.Keeper) (cnt int) {
	store := prefix.NewStore(ctx.KVStore(k.GetStoreKey()), types.TxHashesPrefix)
	iter := store.Iterator(nil, nil)
	defer iter.Close()
	for ; iter.Valid(); iter.Next() {
		cnt++
	}
	return
}
