package keeper_test

import (
	"strconv"
	"testing"

	sdk "github.com/cosmos/cosmos-sdk/types"
	keepertest "github.com/sei-protocol/sei-chain/testutil/keeper"
	"github.com/sei-protocol/sei-chain/x/dex/keeper"
	"github.com/sei-protocol/sei-chain/x/dex/types"
	"github.com/stretchr/testify/require"
)

func createNSettlements(keeper *keeper.Keeper, ctx sdk.Context, n int) []types.Settlements {
	items := make([]types.Settlements, n)
	for i := range items {
		acct := "test_account" + strconv.Itoa(i)
		entry := types.SettlementEntry{
			Account:    acct,
			PriceDenom: "usdc" + strconv.Itoa(i),
			AssetDenom: "sei" + strconv.Itoa(i),
			OrderId:    uint64(i),
		}
		entries := []*types.SettlementEntry{&entry}
		items[i].Entries = entries
		keeper.SetSettlements(ctx, TEST_CONTRACT, "usdc"+strconv.Itoa(i), "sei"+strconv.Itoa(i), items[i])
	}
	return items
}

func TestGetSettlementsStateForAccount(t *testing.T) {
	keeper, ctx := keepertest.DexKeeper(t)
	createNSettlements(keeper, ctx, 1)
	res := keeper.GetSettlementsStateForAccount(ctx, TEST_CONTRACT, "usdc0", "sei0", "test_account0")
	require.Equal(t, 1, len(res))
}

func TestGetAllSettlementsState(t *testing.T) {
	keeper, ctx := keepertest.DexKeeper(t)
	createNSettlements(keeper, ctx, 1)
	res := keeper.GetAllSettlementsState(ctx, TEST_CONTRACT, "usdc0", "sei0", 100)
	require.Equal(t, 1, len(res))
}
