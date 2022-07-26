package query_test

import (
	"testing"

	sdk "github.com/cosmos/cosmos-sdk/types"
	keepertest "github.com/sei-protocol/sei-chain/testutil/keeper"
	"github.com/sei-protocol/sei-chain/x/dex/keeper/query"
	"github.com/sei-protocol/sei-chain/x/dex/types"
	"github.com/stretchr/testify/require"
)

func TestAssetListQuery(t *testing.T) {
	keeper, ctx := keepertest.DexKeeper(t)
	wctx := sdk.WrapSDKContext(ctx)
	item := keepertest.CreateAssetMetadata(keeper, ctx)

	var expectedAssetList []types.AssetMetadata
	expectedAssetList = append(expectedAssetList, item)

	request := types.QueryAssetListRequest{}
	expectedResponse := types.QueryAssetListResponse{
		AssetList: expectedAssetList,
	}
	wrapper := query.KeeperWrapper{Keeper: keeper}
	t.Run("Asset list query", func(t *testing.T) {
		response, err := wrapper.AssetList(wctx, &request)
		require.NoError(t, err)
		require.Equal(t, expectedResponse, *response)
	})
}

func TestAssetMetadataQuery(t *testing.T) {
	keeper, ctx := keepertest.DexKeeper(t)
	wctx := sdk.WrapSDKContext(ctx)
	expectedMetadata := keepertest.CreateAssetMetadata(keeper, ctx)

	request := types.QueryAssetMetadataRequest{
		Denom: "axlusdc",
	}
	expectedResponse := types.QueryAssetMetadataResponse{
		Metadata: &expectedMetadata,
	}
	wrapper := query.KeeperWrapper{Keeper: keeper}
	t.Run("Asset metadata query", func(t *testing.T) {
		response, err := wrapper.AssetMetadata(wctx, &request)
		require.NoError(t, err)
		require.Equal(t, expectedResponse, *response)
	})
}
