package keeper_test

import (
	"testing"

	sdk "github.com/cosmos/cosmos-sdk/types"
	keepertest "github.com/sei-protocol/sei-chain/testutil/keeper"
	"github.com/sei-protocol/sei-chain/x/dex/types"
	"github.com/stretchr/testify/require"
)

func TestGetPlaceSudoMsg(t *testing.T) {
	pair := types.Pair{PriceDenom: TEST_PRICE_DENOM, AssetDenom: TEST_ASSET_DENOM}
	keeper, ctx := keepertest.DexKeeper(t)
	keeper.MemState.GetBlockOrders(TEST_CONTRACT, types.GetPairString(&pair)).AddOrder(
		types.Order{
			Id:                1,
			Price:             sdk.OneDec(),
			Quantity:          sdk.OneDec(),
			PriceDenom:        "USDC",
			AssetDenom:        "ATOM",
			OrderType:         types.OrderType_LIMIT,
			PositionDirection: types.PositionDirection_LONG,
			Data:              "{\"position_effect\":\"OPEN\",\"leverage\":\"1\"}",
		},
	)
	msgs := keeper.GetPlaceSudoMsg(ctx, TEST_CONTRACT, []types.Pair{pair})
	require.Equal(t, 2, len(msgs))
}
