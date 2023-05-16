package keeper_test

import (
	"testing"

	sdk "github.com/cosmos/cosmos-sdk/types"
	keepertest "github.com/sei-protocol/sei-chain/testutil/keeper"
	"github.com/sei-protocol/sei-chain/x/dex/types"
	"github.com/stretchr/testify/require"
)

func TestGetSetOrderCount(t *testing.T) {
	keeper, ctx := keepertest.DexKeeper(t)
	for _, direction := range []types.PositionDirection{
		types.PositionDirection_LONG,
		types.PositionDirection_SHORT,
	} {
		require.Equal(t, uint64(0), keeper.GetOrderCount(ctx, keepertest.TestContract, keepertest.TestPair.PriceDenom, keepertest.TestPair.AssetDenom, direction, sdk.NewDec(1)))
		require.Nil(t, keeper.SetOrderCount(ctx, keepertest.TestContract, keepertest.TestPair.PriceDenom, keepertest.TestPair.AssetDenom, direction, sdk.NewDec(1), 5))
		require.Equal(t, uint64(5), keeper.GetOrderCount(ctx, keepertest.TestContract, keepertest.TestPair.PriceDenom, keepertest.TestPair.AssetDenom, direction, sdk.NewDec(1)))
	}
}
