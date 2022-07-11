package keeper_test

import (
	"strconv"

	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/sei-protocol/sei-chain/x/dex/keeper"
	"github.com/sei-protocol/sei-chain/x/dex/types"
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
