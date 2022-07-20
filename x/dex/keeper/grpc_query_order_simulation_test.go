package keeper_test

import (
	"testing"

	sdk "github.com/cosmos/cosmos-sdk/types"
	keepertest "github.com/sei-protocol/sei-chain/testutil/keeper"
	"github.com/sei-protocol/sei-chain/x/dex/types"
	"github.com/stretchr/testify/require"
)

func TestGetOrderSimulation(t *testing.T) {
	keeper, ctx := keepertest.DexKeeper(t)
	wctx := sdk.WrapSDKContext(ctx)

	testOrder := types.Order{
		Account:           TEST_ACCOUNT,
		ContractAddr:      TEST_CONTRACT,
		PriceDenom:        TEST_PRICE_DENOM,
		AssetDenom:        TEST_ASSET_DENOM,
		Price:             sdk.MustNewDecFromStr("10"),
		Quantity:          sdk.MustNewDecFromStr("5"),
		PositionDirection: types.PositionDirection_LONG,
	}

	// no liquidity
	res, err := keeper.GetOrderSimulation(wctx, &types.QueryOrderSimulationRequest{Order: &testOrder})
	require.Nil(t, err)
	require.Equal(t, sdk.ZeroDec(), *res.ExecutedQuantity)

	// partial liquidity on orderbook
	keeper.SetShortBook(ctx, TEST_CONTRACT, types.ShortBook{
		Price: sdk.MustNewDecFromStr("9"),
		Entry: &types.OrderEntry{
			Price:      sdk.MustNewDecFromStr("9"),
			Quantity:   sdk.MustNewDecFromStr("3"),
			PriceDenom: TEST_PRICE_DENOM,
			AssetDenom: TEST_ASSET_DENOM,
		},
	})
	res, err = keeper.GetOrderSimulation(wctx, &types.QueryOrderSimulationRequest{Order: &testOrder})
	require.Nil(t, err)
	require.Equal(t, sdk.MustNewDecFromStr("3"), *res.ExecutedQuantity)

	// full liquidity on orderbook
	keeper.SetShortBook(ctx, TEST_CONTRACT, types.ShortBook{
		Price: sdk.MustNewDecFromStr("8"),
		Entry: &types.OrderEntry{
			Price:      sdk.MustNewDecFromStr("8"),
			Quantity:   sdk.MustNewDecFromStr("3"),
			PriceDenom: TEST_PRICE_DENOM,
			AssetDenom: TEST_ASSET_DENOM,
		},
	})
	res, err = keeper.GetOrderSimulation(wctx, &types.QueryOrderSimulationRequest{Order: &testOrder})
	require.Nil(t, err)
	require.Equal(t, sdk.MustNewDecFromStr("5"), *res.ExecutedQuantity)

	// liquidity taken by cancel
	keeper.AddNewOrder(ctx, types.Order{
		Id:                1,
		Account:           TEST_ACCOUNT,
		ContractAddr:      TEST_CONTRACT,
		PriceDenom:        TEST_PRICE_DENOM,
		AssetDenom:        TEST_ASSET_DENOM,
		Price:             sdk.MustNewDecFromStr("9"),
		Quantity:          sdk.MustNewDecFromStr("2"),
		PositionDirection: types.PositionDirection_SHORT,
	})
	keeper.MemState.GetBlockCancels(types.ContractAddress(TEST_CONTRACT), types.GetPairString(&TEST_PAIR)).AddCancel(
		types.Cancellation{Id: 1},
	)
	res, err = keeper.GetOrderSimulation(wctx, &types.QueryOrderSimulationRequest{Order: &testOrder})
	require.Nil(t, err)
	require.Equal(t, sdk.MustNewDecFromStr("4"), *res.ExecutedQuantity)

	// liquidity taken by earlier market orders
	keeper.MemState.GetBlockOrders(types.ContractAddress(TEST_CONTRACT), types.GetPairString(&TEST_PAIR)).AddOrder(
		types.Order{
			Account:           TEST_ACCOUNT,
			ContractAddr:      TEST_CONTRACT,
			PriceDenom:        TEST_PRICE_DENOM,
			AssetDenom:        TEST_ASSET_DENOM,
			Price:             sdk.MustNewDecFromStr("11"),
			Quantity:          sdk.MustNewDecFromStr("2"),
			PositionDirection: types.PositionDirection_LONG,
			OrderType:         types.OrderType_MARKET,
		},
	)
	res, err = keeper.GetOrderSimulation(wctx, &types.QueryOrderSimulationRequest{Order: &testOrder})
	require.Nil(t, err)
	require.Equal(t, sdk.MustNewDecFromStr("2"), *res.ExecutedQuantity)
}
