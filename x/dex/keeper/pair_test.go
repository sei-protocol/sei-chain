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
	require.Equal(t, uint64(0), keeper.GetPairCount(ctx, keepertest.TestContract))
	keeper.SetPairCount(ctx, keepertest.TestContract, 1)
	require.Equal(t, uint64(1), keeper.GetPairCount(ctx, keepertest.TestContract))
}

func TestAddGetPair(t *testing.T) {
	keeper, ctx := keepertest.DexKeeper(t)
	keeper.AddRegisteredPair(ctx, keepertest.TestContract, types.Pair{
		PriceDenom:       keepertest.TestPriceDenom,
		AssetDenom:       keepertest.TestAssetDenom,
		PriceTicksize:    &keepertest.TestTicksize,
		QuantityTicksize: &keepertest.TestTicksize,
	})
	require.Equal(t, uint64(1), keeper.GetPairCount(ctx, keepertest.TestContract))
	require.ElementsMatch(t,
		nullify.Fill([]types.Pair{{
			PriceDenom:       keepertest.TestPriceDenom,
			AssetDenom:       keepertest.TestAssetDenom,
			PriceTicksize:    &keepertest.TestTicksize,
			QuantityTicksize: &keepertest.TestTicksize,
		}}),
		nullify.Fill(keeper.GetAllRegisteredPairs(ctx, keepertest.TestContract)),
	)
}
