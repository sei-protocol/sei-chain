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
	keeper.OrderPlacements[TEST_CONTRACT] = map[string]*dex.OrderPlacements{}
	keeper.OrderPlacements[TEST_CONTRACT][pair.String()] = dex.NewOrderPlacements()
	keeper.OrderPlacements[TEST_CONTRACT][pair.String()].Orders = append(
		keeper.OrderPlacements[TEST_CONTRACT][pair.String()].Orders,
		dex.OrderPlacement{
			Id:         1,
			Price:      sdk.OneDec(),
			Quantity:   sdk.OneDec(),
			PriceDenom: TEST_PRICE_DENOM,
			AssetDenom: TEST_ASSET_DENOM,
			OrderType:  types.OrderType_LIMIT,
			Direction:  types.PositionDirection_LONG,
			Effect:     types.PositionEffect_OPEN,
			Leverage:   sdk.OneDec(),
		},
	)
	msgs := keeper.GetPlaceSudoMsg(TEST_CONTRACT, []types.Pair{pair})
	require.Equal(t, 2, len(msgs))
}
