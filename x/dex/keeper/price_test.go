package keeper_test

import (
	"testing"

	keepertest "github.com/sei-protocol/sei-chain/testutil/keeper"
	"github.com/stretchr/testify/require"
)

func TestDeletePriceStateBefore(t *testing.T) {
	keeper, ctx := keepertest.DexKeeper(t)
	keepertest.SeedPriceSnapshot(ctx, keeper, "100", 1)
	keepertest.SeedPriceSnapshot(ctx, keeper, "101", 2)
	keepertest.SeedPriceSnapshot(ctx, keeper, "99", 3)
	keeper.DeletePriceStateBefore(ctx, keepertest.TestContract, 2, keepertest.TestPair)
	prices := keeper.GetAllPrices(ctx, keepertest.TestContract, keepertest.TestPair)
	require.Equal(t, 2, len(prices))
	require.Equal(t, uint64(2), prices[0].SnapshotTimestampInSeconds)
	require.Equal(t, uint64(3), prices[1].SnapshotTimestampInSeconds)
}
