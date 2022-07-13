package keeper_test

import (
	"testing"

	sdk "github.com/cosmos/cosmos-sdk/types"
	keepertest "github.com/sei-protocol/sei-chain/testutil/keeper"
	"github.com/sei-protocol/sei-chain/x/dex/types"
	"github.com/stretchr/testify/require"
)

func TestAssetListQuery(t *testing.T) {
	keeper, ctx := keepertest.DexKeeper(t)
	wctx := sdk.WrapSDKContext(ctx)
	item := createAssetMetadata(keeper, ctx)

	var expectedAssetList []types.AssetMetadata
	expectedAssetList = append(expectedAssetList, item)

	request := types.QueryAssetListRequest{}
	expectedResponse := types.QueryAssetListResponse{
		AssetList: expectedAssetList,
	}
	t.Run("Asset list query", func(t *testing.T) {
		response, err := keeper.AssetList(wctx, &request)
		require.NoError(t, err)
		require.Equal(t, expectedResponse, *response)
	})
}

func TestAssetMetadataQuery(t *testing.T) {
	keeper, ctx := keepertest.DexKeeper(t)
	wctx := sdk.WrapSDKContext(ctx)
	expectedMetadata := createAssetMetadata(keeper, ctx)

	request := types.QueryAssetMetadataRequest{
		Denom: "axlusdc",
	}
	expectedResponse := types.QueryAssetMetadataResponse{
		Metadata: &expectedMetadata,
	}
	t.Run("Asset metadata query", func(t *testing.T) {
		response, err := keeper.AssetMetadata(wctx, &request)
		require.NoError(t, err)
		require.Equal(t, expectedResponse, *response)
	})
}
