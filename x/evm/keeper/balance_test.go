package keeper_test

import (
	"testing"

	"github.com/sei-protocol/sei-chain/testutil/keeper"
	"github.com/sei-protocol/sei-chain/x/evm/types"
	"github.com/stretchr/testify/require"
)

func TestSetGetBalance(t *testing.T) {
	k, ctx := keeper.MockEVMKeeper()
	_, evmAddr := keeper.MockAddressPair()
	k.SetOrDeleteBalance(ctx, evmAddr, 10)
	require.Equal(t, uint64(10), k.GetBalance(ctx, evmAddr))
	k.SetOrDeleteBalance(ctx, evmAddr, 20)
	require.Equal(t, uint64(20), k.GetBalance(ctx, evmAddr))
	k.SetOrDeleteBalance(ctx, evmAddr, 0)
	require.Equal(t, uint64(0), k.GetBalance(ctx, evmAddr))
}

func TestGetBadBalance(t *testing.T) {
	k, ctx := keeper.MockEVMKeeper()
	_, evmAddr := keeper.MockAddressPair()
	store := ctx.KVStore(k.GetStoreKey())
	store.Set(types.BalanceKey(evmAddr), []byte("garbage"))
	require.Panics(t, func() { k.GetBalance(ctx, evmAddr) })
}
