package keeper_test

import (
	"testing"

	sdk "github.com/cosmos/cosmos-sdk/types"
	keepertest "github.com/sei-protocol/sei-chain/testutil/keeper"
	"github.com/sei-protocol/sei-chain/testutil/nullify"
	"github.com/sei-protocol/sei-chain/x/dex/types"
	"github.com/stretchr/testify/require"
)

func TestTriggeredOrderGetByID(t *testing.T) {
	keeper, ctx := keepertest.DexKeeper(t)

	order := types.Order{
		Id:            1,
		Price:         sdk.MustNewDecFromStr("1"),
		Quantity:      sdk.MustNewDecFromStr("1"),
		Nominal:       sdk.MustNewDecFromStr("1"),
		OrderType:     types.OrderType_STOPLOSS,
		TriggerPrice:  sdk.MustNewDecFromStr("4"),
		TriggerStatus: false,
	}
	keeper.SetTriggeredOrder(ctx, keepertest.TestContract, order, keepertest.TestPriceDenom, keepertest.TestAssetDenom)

	got, found := keeper.GetTriggeredOrderByID(ctx, keepertest.TestContract, 1, keepertest.TestPriceDenom, keepertest.TestAssetDenom)
	require.True(t, found)
	require.Equal(t, nullify.Fill(&order), nullify.Fill(&got))

	got, found = keeper.GetTriggeredOrderByID(ctx, keepertest.TestContract, 2, keepertest.TestPriceDenom, keepertest.TestAssetDenom)
	require.True(t, !found)
	require.Equal(t, types.Order{}, got)
}

func TestTriggeredOrderGetAllPair(t *testing.T) {
	keeper, ctx := keepertest.DexKeeper(t)

	o1 := types.Order{
		Id:            1,
		Price:         sdk.MustNewDecFromStr("1"),
		Quantity:      sdk.MustNewDecFromStr("1"),
		Nominal:       sdk.MustNewDecFromStr("1"),
		OrderType:     types.OrderType_STOPLOSS,
		TriggerPrice:  sdk.MustNewDecFromStr("4"),
		TriggerStatus: false,
	}
	o2 := types.Order{
		Id:            2,
		Price:         sdk.MustNewDecFromStr("1"),
		Quantity:      sdk.MustNewDecFromStr("1"),
		Nominal:       sdk.MustNewDecFromStr("1"),
		OrderType:     types.OrderType_STOPLOSS,
		TriggerPrice:  sdk.MustNewDecFromStr("4"),
		TriggerStatus: false,
	}
	o3 := types.Order{
		Id:            3,
		Price:         sdk.MustNewDecFromStr("1"),
		Quantity:      sdk.MustNewDecFromStr("1"),
		Nominal:       sdk.MustNewDecFromStr("1"),
		OrderType:     types.OrderType_STOPLOSS,
		TriggerPrice:  sdk.MustNewDecFromStr("4"),
		TriggerStatus: false,
	}
	keeper.SetTriggeredOrder(ctx, keepertest.TestContract, o1, keepertest.TestPriceDenom, keepertest.TestAssetDenom)
	keeper.SetTriggeredOrder(ctx, keepertest.TestContract, o2, keepertest.TestPriceDenom, keepertest.TestAssetDenom)
	keeper.SetTriggeredOrder(ctx, keepertest.TestContract, o3, "FOO", "BAR")

	orders := keeper.GetAllTriggeredOrdersForPair(ctx, keepertest.TestContract, keepertest.TestPriceDenom, keepertest.TestAssetDenom)
	require.Equal(t, len(orders), 2)
	orders = keeper.GetAllTriggeredOrdersForPair(ctx, keepertest.TestContract, "FOO", "BAR")
	require.Equal(t, len(orders), 1)
}

func TestTriggeredOrderGetAll(t *testing.T) {
	keeper, ctx := keepertest.DexKeeper(t)

	o1 := types.Order{
		Id:            1,
		Price:         sdk.MustNewDecFromStr("1"),
		Quantity:      sdk.MustNewDecFromStr("1"),
		Nominal:       sdk.MustNewDecFromStr("1"),
		OrderType:     types.OrderType_STOPLOSS,
		TriggerPrice:  sdk.MustNewDecFromStr("4"),
		TriggerStatus: false,
	}
	o2 := types.Order{
		Id:            2,
		Price:         sdk.MustNewDecFromStr("1"),
		Quantity:      sdk.MustNewDecFromStr("1"),
		Nominal:       sdk.MustNewDecFromStr("1"),
		OrderType:     types.OrderType_STOPLOSS,
		TriggerPrice:  sdk.MustNewDecFromStr("4"),
		TriggerStatus: false,
	}
	o3 := types.Order{
		Id:            3,
		Price:         sdk.MustNewDecFromStr("1"),
		Quantity:      sdk.MustNewDecFromStr("1"),
		Nominal:       sdk.MustNewDecFromStr("1"),
		OrderType:     types.OrderType_STOPLOSS,
		TriggerPrice:  sdk.MustNewDecFromStr("4"),
		TriggerStatus: false,
	}
	keeper.SetTriggeredOrder(ctx, keepertest.TestContract, o1, keepertest.TestPriceDenom, keepertest.TestAssetDenom)
	keeper.SetTriggeredOrder(ctx, keepertest.TestContract, o2, keepertest.TestPriceDenom, keepertest.TestAssetDenom)
	keeper.SetTriggeredOrder(ctx, keepertest.TestContract, o3, "FOO", "BAR")

	orders := keeper.GetAllTriggeredOrders(ctx, keepertest.TestContract)
	require.Equal(t, len(orders), 3)
}

func TestTriggeredOrderSetOverwrite(t *testing.T) {
	keeper, ctx := keepertest.DexKeeper(t)

	order := types.Order{
		Id:            1,
		Price:         sdk.MustNewDecFromStr("1"),
		Quantity:      sdk.MustNewDecFromStr("1"),
		Nominal:       sdk.MustNewDecFromStr("1"),
		OrderType:     types.OrderType_STOPLOSS,
		TriggerPrice:  sdk.MustNewDecFromStr("4"),
		TriggerStatus: false,
	}
	overwriteOrder := types.Order{
		Id:            1,
		Price:         sdk.MustNewDecFromStr("1"),
		Quantity:      sdk.MustNewDecFromStr("1"),
		Nominal:       sdk.MustNewDecFromStr("1"),
		OrderType:     types.OrderType_STOPLIMIT,
		TriggerPrice:  sdk.MustNewDecFromStr("4"),
		TriggerStatus: false,
	}

	keeper.SetTriggeredOrder(ctx, keepertest.TestContract, order, keepertest.TestPriceDenom, keepertest.TestAssetDenom)
	got, found := keeper.GetTriggeredOrderByID(ctx, keepertest.TestContract, 1, keepertest.TestPriceDenom, keepertest.TestAssetDenom)
	require.True(t, found)
	require.Equal(t, nullify.Fill(&order), nullify.Fill(&got))

	keeper.SetTriggeredOrder(ctx, keepertest.TestContract, overwriteOrder, keepertest.TestPriceDenom, keepertest.TestAssetDenom)
	got, found = keeper.GetTriggeredOrderByID(ctx, keepertest.TestContract, 1, keepertest.TestPriceDenom, keepertest.TestAssetDenom)
	require.True(t, found)
	require.Equal(t, nullify.Fill(&overwriteOrder), nullify.Fill(&got))
}

func TestTriggeredOrderRemove(t *testing.T) {
	keeper, ctx := keepertest.DexKeeper(t)

	order := types.Order{
		Id:            1,
		Price:         sdk.MustNewDecFromStr("1"),
		Quantity:      sdk.MustNewDecFromStr("1"),
		Nominal:       sdk.MustNewDecFromStr("1"),
		OrderType:     types.OrderType_STOPLOSS,
		TriggerPrice:  sdk.MustNewDecFromStr("4"),
		TriggerStatus: false,
	}

	keeper.SetTriggeredOrder(ctx, keepertest.TestContract, order, keepertest.TestPriceDenom, keepertest.TestAssetDenom)
	got, found := keeper.GetTriggeredOrderByID(ctx, keepertest.TestContract, 1, keepertest.TestPriceDenom, keepertest.TestAssetDenom)
	require.True(t, found)
	require.Equal(t, nullify.Fill(&order), nullify.Fill(&got))

	keeper.RemoveTriggeredOrder(ctx, keepertest.TestContract, 1, keepertest.TestPriceDenom, keepertest.TestAssetDenom)

	got, found = keeper.GetTriggeredOrderByID(ctx, keepertest.TestContract, 1, keepertest.TestPriceDenom, keepertest.TestAssetDenom)
	require.True(t, !found)
	require.Equal(t, types.Order{}, got)
}
