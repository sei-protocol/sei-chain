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

func TestAddOrderFromOrderPlacement(t *testing.T) {
	pair := types.Pair{PriceDenom: TEST_PRICE_DENOM, AssetDenom: TEST_ASSET_DENOM}
	keeper, _ := keepertest.DexKeeper(t)
	keeper.DepositInfo[TEST_CONTRACT] = dex.NewDepositInfo()
	keeper.OrderPlacements[TEST_CONTRACT] = map[string]*dex.OrderPlacements{}
	keeper.OrderPlacements[TEST_CONTRACT][pair.String()] = dex.NewOrderPlacements()
	keeper.Orders[TEST_CONTRACT] = map[string]*dex.Orders{}
	keeper.Orders[TEST_CONTRACT][pair.String()] = dex.NewOrders()

	limitOrderPlacement := dex.OrderPlacement{
		Id:         0,
		Price:      sdk.OneDec(),
		Quantity:   sdk.OneDec(),
		PriceDenom: TEST_PRICE_DENOM,
		AssetDenom: TEST_ASSET_DENOM,
		OrderType:  types.OrderType_LIMIT,
		Direction:  types.PositionDirection_LONG,
		Effect:     types.PositionEffect_OPEN,
		Leverage:   sdk.OneDec(),
	}
	marketOrderPlacement := dex.OrderPlacement{
		Id:         0,
		Price:      sdk.OneDec(),
		Quantity:   sdk.OneDec(),
		PriceDenom: TEST_PRICE_DENOM,
		AssetDenom: TEST_ASSET_DENOM,
		OrderType:  types.OrderType_MARKET,
		Direction:  types.PositionDirection_LONG,
		Effect:     types.PositionEffect_OPEN,
		Leverage:   sdk.OneDec(),
	}
	liquidationOrderPlacement := dex.OrderPlacement{
		Id:         0,
		Price:      sdk.OneDec(),
		Quantity:   sdk.OneDec(),
		PriceDenom: TEST_PRICE_DENOM,
		AssetDenom: TEST_ASSET_DENOM,
		OrderType:  types.OrderType_LIQUIDATION,
		Direction:  types.PositionDirection_LONG,
		Effect:     types.PositionEffect_OPEN,
		Leverage:   sdk.OneDec(),
	}

	keeper.AddOrderFromOrderPlacement(TEST_CONTRACT, pair.String(), limitOrderPlacement)
	keeper.AddOrderFromOrderPlacement(TEST_CONTRACT, pair.String(), marketOrderPlacement)
	keeper.AddOrderFromOrderPlacement(TEST_CONTRACT, pair.String(), liquidationOrderPlacement)

	require.Equal(t, 1, len(keeper.Orders[TEST_CONTRACT][pair.String()].LimitBuys))
	require.Equal(t, 2, len(keeper.Orders[TEST_CONTRACT][pair.String()].MarketBuys))
}
