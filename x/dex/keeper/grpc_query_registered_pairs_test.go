package keeper_test

import (
	"testing"

	sdk "github.com/cosmos/cosmos-sdk/types"
	keepertest "github.com/sei-protocol/sei-chain/testutil/keeper"
	"github.com/sei-protocol/sei-chain/x/dex/types"
	"github.com/stretchr/testify/require"
)

func TestRegisteredPairsQuery(t *testing.T) {
	keeper, ctx := keepertest.DexKeeper(t)
	wctx := sdk.WrapSDKContext(ctx)
	expectedPair := types.Pair{
		PriceDenom: TEST_PRICE_DENOM,
		AssetDenom: TEST_ASSET_DENOM,
		Ticksize:   &TEST_TICKSIZE,
	}
	keeper.AddRegisteredPair(ctx, TEST_CONTRACT, expectedPair)

	var expectedRegisteredPairs []types.Pair
	expectedRegisteredPairs = append(expectedRegisteredPairs, expectedPair)

	request := types.QueryRegisteredPairsRequest{
		ContractAddr: TEST_CONTRACT,
	}
	expectedResponse := types.QueryRegisteredPairsResponse{
		Pairs: expectedRegisteredPairs,
	}
	t.Run("Registered Pairs query", func(t *testing.T) {
		response, err := keeper.GetRegisteredPairs(wctx, &request)
		require.NoError(t, err)
		require.Equal(t, expectedResponse, *response)
	})
}
