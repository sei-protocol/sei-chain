package keeper_test

import (
	"testing"

	sdk "github.com/cosmos/cosmos-sdk/types"
	keepertest "github.com/sei-protocol/sei-chain/testutil/keeper"
	banktypes "github.com/cosmos/cosmos-sdk/x/bank/types"
	"github.com/stretchr/testify/require"
	"github.com/sei-protocol/sei-chain/testutil/nullify"
	"github.com/sei-protocol/sei-chain/x/dex/keeper"
	"github.com/sei-protocol/sei-chain/x/dex/types"
)

func createAssetMetadata(keeper *keeper.Keeper, ctx sdk.Context) types.AssetMetadata {
	ibc_info := types.AssetIBCInfo{
		SourceChannel: "channel-1",
		DstChannel: "channel-2",
		SourceDenom: "uusdc",
		SourceChainID: "axelar",
	}

	denom_unit := banktypes.DenomUnit{
		Denom: "ibc/D189335C6E4A68B513C10AB227BF1C1D38C746766278BA3EEB4FB14124F1D858",
		Exponent: 0,
		Aliases: []string{"axlusdc", "usdc"},
	}

	var denom_units []*banktypes.DenomUnit
	denom_units = append(denom_units, &denom_unit)

	metadata := banktypes.Metadata{
		Description: "Circle's stablecoin on Axelar",
		DenomUnits: denom_units,
		Base: "ibc/D189335C6E4A68B513C10AB227BF1C1D38C746766278BA3EEB4FB14124F1D858",
		Name: "USD Coin",
		Display: "axlusdc",
		Symbol: "USDC",
	}

	item := types.AssetMetadata{
		IbcInfo: &ibc_info,
		TypeAsset: "erc20",
		Metadata: metadata,
	}

	keeper.SetAssetMetadata(ctx, item)

	return item
}

func TestAssetListGet(t *testing.T) {
	keeper, ctx := keepertest.DexKeeper(t)
	item := createAssetMetadata(keeper, ctx)

	var expected_asset_list []types.AssetMetadata
	expected_asset_list = append(expected_asset_list, item)

	asset_list := keeper.GetAllAssetMetadata(ctx)

	// First test get all asset list
	require.ElementsMatch(t,
		nullify.Fill(expected_asset_list),
		nullify.Fill(asset_list),
	)

	// Test not found asset Denom
	_, found :=  keeper.GetAssetMetadataByDenom(ctx, "denomNotInAssetList123")
	require.False(t, found)

	// Test get specific Denom
	val, found := keeper.GetAssetMetadataByDenom(ctx, "axlusdc")
	require.True(t, found)
	require.Equal(t, item, val)
}
