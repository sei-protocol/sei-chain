package keeper_test

import (
	"testing"

	sdk "github.com/cosmos/cosmos-sdk/types"
	keepertest "github.com/sei-protocol/sei-chain/testutil/keeper"
	dex "github.com/sei-protocol/sei-chain/x/dex/cache"
	"github.com/sei-protocol/sei-chain/x/dex/types"
	"github.com/stretchr/testify/require"
)

func TestGetPlaceSudoMsg(t *testing.T) {
	pair := types.Pair{PriceDenom: TEST_PRICE_DENOM, AssetDenom: TEST_ASSET_DENOM}
	keeper, _ := keepertest.DexKeeper(t)
	keeper.DepositInfo[TEST_CONTRACT] = dex.NewDepositInfo()
	keeper.BlockOrders[TEST_CONTRACT] = map[types.PairString]*dex.BlockOrders{}
	emptyBlockOrder := dex.BlockOrders([]types.Order{})
	keeper.BlockOrders[TEST_CONTRACT][types.PairString(pair.String())] = &emptyBlockOrder
	keeper.BlockOrders[TEST_CONTRACT][types.PairString(pair.String())].AddOrder(
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
	msgs := keeper.GetPlaceSudoMsg(TEST_CONTRACT, []types.Pair{pair})
	require.Equal(t, 2, len(msgs))
}
