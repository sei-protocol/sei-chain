package dex_test

import (
	"testing"

	keepertest "github.com/sei-protocol/sei-chain/testutil/keeper"
	dex "github.com/sei-protocol/sei-chain/x/dex/cache"
	"github.com/sei-protocol/sei-chain/x/dex/types"
	"github.com/stretchr/testify/require"
)

func TestCancelGetIdsToCancel(t *testing.T) {
	keeper, ctx := keepertest.DexKeeper(t)
	stateOne := dex.NewMemState(keeper.GetMemStoreKey())
	stateOne.GetBlockCancels(ctx, types.ContractAddress(TEST_CONTRACT), keepertest.TestPair).Add(&types.Cancellation{
		Id:           1,
		Creator:      "abc",
		ContractAddr: TEST_CONTRACT,
	})
	ids := stateOne.GetBlockCancels(ctx, types.ContractAddress(TEST_CONTRACT), keepertest.TestPair).GetIdsToCancel()
	require.Equal(t, 1, len(ids))
	require.Equal(t, uint64(1), ids[0])
}

func TestCancelGetCancels(t *testing.T) {
	keeper, ctx := keepertest.DexKeeper(t)
	stateOne := dex.NewMemState(keeper.GetMemStoreKey())
	stateOne.GetBlockCancels(ctx, types.ContractAddress(TEST_CONTRACT), keepertest.TestPair).Add(&types.Cancellation{
		Id:           1,
		Creator:      "abc",
		ContractAddr: TEST_CONTRACT,
	})
	stateOne.GetBlockCancels(ctx, types.ContractAddress(TEST_CONTRACT), keepertest.TestPair).Add(&types.Cancellation{
		Id:           2,
		Creator:      "def",
		ContractAddr: TEST_CONTRACT,
	})
	stateOne.GetBlockCancels(ctx, types.ContractAddress(TEST_CONTRACT), keepertest.TestPair).Add(&types.Cancellation{
		Id:           3,
		Creator:      "efg",
		ContractAddr: TEST_CONTRACT,
	})
	stateOne.GetBlockCancels(ctx, types.ContractAddress(TEST_CONTRACT), keepertest.TestPair).Add(&types.Cancellation{
		Id:           4,
		Creator:      "efg",
		ContractAddr: TEST_CONTRACT,
	})

	cancels := stateOne.GetBlockCancels(ctx, types.ContractAddress(TEST_CONTRACT), keepertest.TestPair).Get()
	require.Equal(t, 4, len(cancels))
	require.True(t, stateOne.GetBlockCancels(ctx, types.ContractAddress(TEST_CONTRACT), keepertest.TestPair).Has(&types.Cancellation{
		Id:           1,
		Creator:      "abc",
		ContractAddr: TEST_CONTRACT,
	}))
	require.True(t, stateOne.GetBlockCancels(ctx, types.ContractAddress(TEST_CONTRACT), keepertest.TestPair).Has(&types.Cancellation{
		Id:           2,
		Creator:      "def",
		ContractAddr: TEST_CONTRACT,
	}))
	require.True(t, stateOne.GetBlockCancels(ctx, types.ContractAddress(TEST_CONTRACT), keepertest.TestPair).Has(&types.Cancellation{
		Id:           3,
		Creator:      "efg",
		ContractAddr: TEST_CONTRACT,
	}))
	require.True(t, stateOne.GetBlockCancels(ctx, types.ContractAddress(TEST_CONTRACT), keepertest.TestPair).Has(&types.Cancellation{
		Id:           4,
		Creator:      "efg",
		ContractAddr: TEST_CONTRACT,
	}))
	require.False(t, stateOne.GetBlockCancels(ctx, types.ContractAddress(TEST_CONTRACT), keepertest.TestPair).Has(&types.Cancellation{
		Id:           5,
		Creator:      "efg",
		ContractAddr: TEST_CONTRACT,
	}))
}
