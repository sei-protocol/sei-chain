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
		pd := types.Denom(2 * i)
		ad := types.Denom(2*i + 1)
		entry := types.SettlementEntry{
			Account:    acct,
			PriceDenom: pd,
			AssetDenom: ad,
		}
		entries := []*types.SettlementEntry{&entry}
		items[i].Entries = entries
		keeper.SetSettlements(ctx, TEST_CONTRACT, pd, ad, items[i])
	}
	return items
}
