package keeper_test

import (
	"testing"

	sdk "github.com/cosmos/cosmos-sdk/types"
	keepertest "github.com/sei-protocol/sei-chain/testutil/keeper"
	dexkeeper "github.com/sei-protocol/sei-chain/x/dex/keeper"
	"github.com/sei-protocol/sei-chain/x/dex/types"
	"github.com/stretchr/testify/require"
)

func seedPriceSnapshot(ctx sdk.Context, k *dexkeeper.Keeper, price string, timestamp uint64) {
	priceSnapshot := types.Price{
		SnapshotTimestampInSeconds: timestamp,
		Price:                      sdk.MustNewDecFromStr(price),
		Pair:                       &TEST_PAIR,
	}
	k.SetPriceState(ctx, priceSnapshot, TEST_CONTRACT)
}

func TestDeletePriceStateBefore(t *testing.T) {
	keeper, ctx := keepertest.DexKeeper(t)
	seedPriceSnapshot(ctx, keeper, "100", 1)
	seedPriceSnapshot(ctx, keeper, "101", 2)
	seedPriceSnapshot(ctx, keeper, "99", 3)
	keeper.DeletePriceStateBefore(ctx, TEST_CONTRACT, 2, TEST_PAIR)
	prices := keeper.GetAllPrices(ctx, TEST_CONTRACT, TEST_PAIR)
	require.Equal(t, 2, len(prices))
	require.Equal(t, uint64(2), prices[0].SnapshotTimestampInSeconds)
	require.Equal(t, uint64(3), prices[1].SnapshotTimestampInSeconds)
}
