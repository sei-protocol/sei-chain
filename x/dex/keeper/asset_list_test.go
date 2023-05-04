package keeper_test

import (
	"testing"

	keepertest "github.com/sei-protocol/sei-chain/testutil/keeper"
	"github.com/sei-protocol/sei-chain/testutil/nullify"
	"github.com/sei-protocol/sei-chain/x/dex/types"
	"github.com/stretchr/testify/require"
)

func TestAssetListGet(t *testing.T) {
	keeper, ctx := keepertest.DexKeeper(t)
	item := keepertest.CreateAssetMetadata(keeper, ctx)

	var expected_asset_list []types.AssetMetadata
	expected_asset_list = append(expected_asset_list, item)

	asset_list := keeper.GetAllAssetMetadata(ctx)

	// First test get all asset list
	require.ElementsMatch(t,
		nullify.Fill(expected_asset_list),
		nullify.Fill(asset_list),
	)

	// Test not found asset Denom
	_, found := keeper.GetAssetMetadataByDenom(ctx, "denomNotInAssetList123")
	require.False(t, found)

	// Test get specific Denom
	val, found := keeper.GetAssetMetadataByDenom(ctx, "axlusdc")
	require.True(t, found)
	require.Equal(t, item, val)
}
