package keeper_test

import (
	"testing"

	keepertest "github.com/sei-protocol/sei-chain/testutil/keeper"
	"github.com/sei-protocol/sei-chain/x/dex/types"
	"github.com/stretchr/testify/require"
)

func TestGetSettlementsStateForAccount(t *testing.T) {
	keeper, ctx := keepertest.DexKeeper(t)
	keepertest.CreateNSettlements(keeper, ctx, 1)
	res := keeper.GetSettlementsStateForAccount(ctx, keepertest.TestContract, "usdc0", "sei0", "test_account0")
	require.Equal(t, 1, len(res))
}

func TestGetAllSettlementsState(t *testing.T) {
	keeper, ctx := keepertest.DexKeeper(t)
	keepertest.CreateNSettlements(keeper, ctx, 1)
	res := keeper.GetAllSettlementsState(ctx, keepertest.TestContract, "usdc0", "sei0", 100)
	require.Equal(t, 1, len(res))
}

func TestSetSettlements(t *testing.T) {
	keeper, ctx := keepertest.DexKeeper(t)
	settlements := types.Settlements{
		Epoch: 0,
		Entries: []*types.SettlementEntry{
			{
				OrderId: 1,
				Account: "abc",
			},
			{
				OrderId: 2,
				Account: "def",
			},
		},
	}
	keeper.SetSettlements(ctx, keepertest.TestContract, keepertest.TestPriceDenom, keepertest.TestAssetDenom, settlements)
	settlementsOrder1, found := keeper.GetSettlementsState(ctx, keepertest.TestContract, keepertest.TestPriceDenom, keepertest.TestAssetDenom, "abc", 1)
	require.True(t, found)
	require.Equal(t, 1, len(settlementsOrder1.Entries))
	settlementsOrder2, found := keeper.GetSettlementsState(ctx, keepertest.TestContract, keepertest.TestPriceDenom, keepertest.TestAssetDenom, "def", 2)
	require.True(t, found)
	require.Equal(t, 1, len(settlementsOrder2.Entries))
}
