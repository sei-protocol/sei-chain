package keeper_test

import (
	"testing"

	keepertest "github.com/sei-protocol/sei-chain/testutil/keeper"
	"github.com/sei-protocol/sei-chain/x/dex/types"
	"github.com/stretchr/testify/require"
)

func TestPlaceLiquidationOrders(t *testing.T) {
	keeper, ctx := keepertest.DexKeeper(t)
	liquidationOrder := types.Order{
		PriceDenom: TEST_PAIR.PriceDenom,
		AssetDenom: TEST_PAIR.AssetDenom,
	}
	keeper.PlaceLiquidationOrders(ctx, TEST_CONTRACT, []types.Order{liquidationOrder})
	require.Equal(t, 1, len(*keeper.MemState.GetBlockOrders(types.ContractAddress(TEST_CONTRACT), types.GetPairString(&TEST_PAIR))))
}
