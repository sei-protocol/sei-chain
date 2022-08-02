package query_test

import (
	"testing"

	sdk "github.com/cosmos/cosmos-sdk/types"
	keepertest "github.com/sei-protocol/sei-chain/testutil/keeper"
	"github.com/sei-protocol/sei-chain/x/dex/keeper/query"
	"github.com/sei-protocol/sei-chain/x/dex/types"
	"github.com/sei-protocol/sei-chain/x/dex/types/utils"
	"github.com/stretchr/testify/require"
)

func TestGetOrderSimulation(t *testing.T) {
	keeper, ctx := keepertest.DexKeeper(t)
	wrapper := query.KeeperWrapper{Keeper: keeper}
	wctx := sdk.WrapSDKContext(ctx)

	testOrder := types.Order{
		Account:           keepertest.TestAccount,
		ContractAddr:      keepertest.TestContract,
		PriceDenom:        keepertest.TestPriceDenom,
		AssetDenom:        keepertest.TestAssetDenom,
		Price:             sdk.MustNewDecFromStr("10"),
		Quantity:          sdk.MustNewDecFromStr("5"),
		PositionDirection: types.PositionDirection_LONG,
	}

	// no liquidity
	res, err := wrapper.GetOrderSimulation(wctx, &types.QueryOrderSimulationRequest{Order: &testOrder})
	require.Nil(t, err)
	require.Equal(t, sdk.ZeroDec(), *res.ExecutedQuantity)

	// partial liquidity on orderbook
	keeper.SetShortBook(ctx, keepertest.TestContract, types.ShortBook{
		Price: sdk.MustNewDecFromStr("9"),
		Entry: &types.OrderEntry{
			Price:      sdk.MustNewDecFromStr("9"),
			Quantity:   sdk.MustNewDecFromStr("3"),
			PriceDenom: keepertest.TestPriceDenom,
			AssetDenom: keepertest.TestAssetDenom,
		},
	})
	res, err = wrapper.GetOrderSimulation(wctx, &types.QueryOrderSimulationRequest{Order: &testOrder})
	require.Nil(t, err)
	require.Equal(t, sdk.MustNewDecFromStr("3"), *res.ExecutedQuantity)

	// full liquidity on orderbook
	keeper.SetShortBook(ctx, keepertest.TestContract, types.ShortBook{
		Price: sdk.MustNewDecFromStr("8"),
		Entry: &types.OrderEntry{
			Price:      sdk.MustNewDecFromStr("8"),
			Quantity:   sdk.MustNewDecFromStr("3"),
			PriceDenom: keepertest.TestPriceDenom,
			AssetDenom: keepertest.TestAssetDenom,
		},
	})
	res, err = wrapper.GetOrderSimulation(wctx, &types.QueryOrderSimulationRequest{Order: &testOrder})
	require.Nil(t, err)
	require.Equal(t, sdk.MustNewDecFromStr("5"), *res.ExecutedQuantity)

	// liquidity taken by cancel
	keeper.AddNewOrder(ctx, types.Order{
		Id:                1,
		Account:           keepertest.TestAccount,
		ContractAddr:      keepertest.TestContract,
		PriceDenom:        keepertest.TestPriceDenom,
		AssetDenom:        keepertest.TestAssetDenom,
		Price:             sdk.MustNewDecFromStr("9"),
		Quantity:          sdk.MustNewDecFromStr("2"),
		PositionDirection: types.PositionDirection_SHORT,
	})
	keeper.MemState.GetBlockCancels(utils.ContractAddress(keepertest.TestContract), utils.GetPairString(&keepertest.TestPair)).Add(
		&types.Cancellation{Id: 1},
	)
	res, err = wrapper.GetOrderSimulation(wctx, &types.QueryOrderSimulationRequest{Order: &testOrder})
	require.Nil(t, err)
	require.Equal(t, sdk.MustNewDecFromStr("4"), *res.ExecutedQuantity)

	// liquidity taken by earlier market orders
	keeper.MemState.GetBlockOrders(utils.ContractAddress(keepertest.TestContract), utils.GetPairString(&keepertest.TestPair)).Add(
		&types.Order{
			Account:           keepertest.TestAccount,
			ContractAddr:      keepertest.TestContract,
			PriceDenom:        keepertest.TestPriceDenom,
			AssetDenom:        keepertest.TestAssetDenom,
			Price:             sdk.MustNewDecFromStr("11"),
			Quantity:          sdk.MustNewDecFromStr("2"),
			PositionDirection: types.PositionDirection_LONG,
			OrderType:         types.OrderType_MARKET,
		},
	)
	res, err = wrapper.GetOrderSimulation(wctx, &types.QueryOrderSimulationRequest{Order: &testOrder})
	require.Nil(t, err)
	require.Equal(t, sdk.MustNewDecFromStr("2"), *res.ExecutedQuantity)
}
