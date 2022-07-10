package keeper_test

import (
	"testing"

	keepertest "github.com/sei-protocol/sei-chain/testutil/keeper"
	"github.com/sei-protocol/sei-chain/testutil/nullify"
	"github.com/sei-protocol/sei-chain/x/dex/types"
	"github.com/stretchr/testify/require"
)

func TestSetGetPairCount(t *testing.T) {
	keeper, ctx := keepertest.DexKeeper(t)
	require.Equal(t, uint64(0), keeper.GetPairCount(ctx, TEST_CONTRACT))
	keeper.SetPairCount(ctx, TEST_CONTRACT, 1)
	require.Equal(t, uint64(1), keeper.GetPairCount(ctx, TEST_CONTRACT))
}

func TestAddGetPair(t *testing.T) {
	keeper, ctx := keepertest.DexKeeper(t)
	keeper.AddRegisteredPair(ctx, TEST_CONTRACT, types.Pair{
		PriceDenom: TEST_PRICE_DENOM,
		AssetDenom: TEST_ASSET_DENOM,
		Ticksize:   &TEST_TICKSIZE,
	})
	require.Equal(t, uint64(1), keeper.GetPairCount(ctx, TEST_CONTRACT))
	require.ElementsMatch(t,
		nullify.Fill([]types.Pair{{
			PriceDenom: TEST_PRICE_DENOM,
			AssetDenom: TEST_ASSET_DENOM,
			Ticksize:   &TEST_TICKSIZE,
		}}),
		nullify.Fill(keeper.GetAllRegisteredPairs(ctx, TEST_CONTRACT)),
	)
}
