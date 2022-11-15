package dex_test

import (
	"testing"

	"github.com/cosmos/cosmos-sdk/store/prefix"
	sdk "github.com/cosmos/cosmos-sdk/types"
	keepertest "github.com/sei-protocol/sei-chain/testutil/keeper"
	dex "github.com/sei-protocol/sei-chain/x/dex/cache"
	"github.com/sei-protocol/sei-chain/x/dex/types"
	"github.com/sei-protocol/sei-chain/x/dex/types/utils"
	"github.com/sei-protocol/sei-chain/x/dex/types/wasm"
	"github.com/stretchr/testify/require"
)

func TestOrderFilterByAccount(t *testing.T) {
	keeper, ctx := keepertest.DexKeeper(t)
	orders := dex.NewOrders(
		prefix.NewStore(
			ctx.KVStore(keeper.GetStoreKey()),
			[]byte("some prefix"),
		),
	)
	order := types.Order{
		Id:      1,
		Account: "abc",
	}
	orders.Add(&order)
	orders.FilterByAccount("abc")
	require.Equal(t, 0, len(orders.Get()))
}

func TestMarkFailedToPlace(t *testing.T) {
	keeper, ctx := keepertest.DexKeeper(t)
	stateOne := dex.NewMemState(keeper.GetStoreKey())
	stateOne.GetBlockOrders(ctx, utils.ContractAddress(TEST_CONTRACT), utils.PairString(TEST_PAIR)).Add(&types.Order{
		Id:           1,
		Account:      "test",
		ContractAddr: TEST_CONTRACT,
	})
	unsuccessfulOrder := wasm.UnsuccessfulOrder{
		ID:     1,
		Reason: "some reason",
	}
	stateOne.GetBlockOrders(ctx, utils.ContractAddress(TEST_CONTRACT), utils.PairString(TEST_PAIR)).MarkFailedToPlace([]wasm.UnsuccessfulOrder{unsuccessfulOrder})
	require.Equal(t, types.OrderStatus_FAILED_TO_PLACE,
		stateOne.GetBlockOrders(ctx, utils.ContractAddress(TEST_CONTRACT), utils.PairString(TEST_PAIR)).Get()[0].Status)
	require.Equal(t, "some reason",
		stateOne.GetBlockOrders(ctx, utils.ContractAddress(TEST_CONTRACT), utils.PairString(TEST_PAIR)).Get()[0].StatusDescription)
}

func TestGetSortedMarketOrders(t *testing.T) {
	keeper, ctx := keepertest.DexKeeper(t)
	stateOne := dex.NewMemState(keeper.GetStoreKey())
	stateOne.GetBlockOrders(ctx, utils.ContractAddress(TEST_CONTRACT), utils.PairString(TEST_PAIR)).Add(&types.Order{
		Id:                1,
		Account:           "test",
		ContractAddr:      TEST_CONTRACT,
		PositionDirection: types.PositionDirection_LONG,
		OrderType:         types.OrderType_LIQUIDATION,
		Price:             sdk.MustNewDecFromStr("150"),
	})
	stateOne.GetBlockOrders(ctx, utils.ContractAddress(TEST_CONTRACT), utils.PairString(TEST_PAIR)).Add(&types.Order{
		Id:                2,
		Account:           "test",
		ContractAddr:      TEST_CONTRACT,
		PositionDirection: types.PositionDirection_LONG,
		OrderType:         types.OrderType_MARKET,
		Price:             sdk.MustNewDecFromStr("100"),
	})
	stateOne.GetBlockOrders(ctx, utils.ContractAddress(TEST_CONTRACT), utils.PairString(TEST_PAIR)).Add(&types.Order{
		Id:                3,
		Account:           "test",
		ContractAddr:      TEST_CONTRACT,
		PositionDirection: types.PositionDirection_LONG,
		OrderType:         types.OrderType_MARKET,
		Price:             sdk.MustNewDecFromStr("0"),
	})
	stateOne.GetBlockOrders(ctx, utils.ContractAddress(TEST_CONTRACT), utils.PairString(TEST_PAIR)).Add(&types.Order{
		Id:                4,
		Account:           "test",
		ContractAddr:      TEST_CONTRACT,
		PositionDirection: types.PositionDirection_SHORT,
		OrderType:         types.OrderType_LIQUIDATION,
		Price:             sdk.MustNewDecFromStr("100"),
	})
	stateOne.GetBlockOrders(ctx, utils.ContractAddress(TEST_CONTRACT), utils.PairString(TEST_PAIR)).Add(&types.Order{
		Id:                5,
		Account:           "test",
		ContractAddr:      TEST_CONTRACT,
		PositionDirection: types.PositionDirection_SHORT,
		OrderType:         types.OrderType_MARKET,
		Price:             sdk.MustNewDecFromStr("80"),
	})
	stateOne.GetBlockOrders(ctx, utils.ContractAddress(TEST_CONTRACT), utils.PairString(TEST_PAIR)).Add(&types.Order{
		Id:                6,
		Account:           "test",
		ContractAddr:      TEST_CONTRACT,
		PositionDirection: types.PositionDirection_SHORT,
		OrderType:         types.OrderType_MARKET,
		Price:             sdk.MustNewDecFromStr("0"),
	})
	stateOne.GetBlockOrders(ctx, utils.ContractAddress(TEST_CONTRACT), utils.PairString(TEST_PAIR)).Add(&types.Order{
		Id:                7,
		Account:           "test",
		ContractAddr:      TEST_CONTRACT,
		PositionDirection: types.PositionDirection_LONG,
		OrderType:         types.OrderType_LIMIT,
		Price:             sdk.MustNewDecFromStr("100"),
	})
	stateOne.GetBlockOrders(ctx, utils.ContractAddress(TEST_CONTRACT), utils.PairString(TEST_PAIR)).Add(&types.Order{
		Id:                8,
		Account:           "test",
		ContractAddr:      TEST_CONTRACT,
		PositionDirection: types.PositionDirection_SHORT,
		OrderType:         types.OrderType_LIMIT,
		Price:             sdk.MustNewDecFromStr("100"),
	})

	marketBuysWithLiquidation := stateOne.GetBlockOrders(
		ctx, utils.ContractAddress(TEST_CONTRACT), utils.PairString(TEST_PAIR)).GetSortedMarketOrders(
		types.PositionDirection_LONG, true,
	)
	require.Equal(t, uint64(3), marketBuysWithLiquidation[0].Id)
	require.Equal(t, uint64(1), marketBuysWithLiquidation[1].Id)
	require.Equal(t, uint64(2), marketBuysWithLiquidation[2].Id)

	marketBuysWithoutLiquidation := stateOne.GetBlockOrders(
		ctx, utils.ContractAddress(TEST_CONTRACT), utils.PairString(TEST_PAIR)).GetSortedMarketOrders(
		types.PositionDirection_LONG, false,
	)
	require.Equal(t, uint64(3), marketBuysWithoutLiquidation[0].Id)
	require.Equal(t, uint64(2), marketBuysWithoutLiquidation[1].Id)

	marketSellsWithLiquidation := stateOne.GetBlockOrders(
		ctx, utils.ContractAddress(TEST_CONTRACT), utils.PairString(TEST_PAIR)).GetSortedMarketOrders(
		types.PositionDirection_SHORT, true,
	)
	require.Equal(t, uint64(6), marketSellsWithLiquidation[0].Id)
	require.Equal(t, uint64(5), marketSellsWithLiquidation[1].Id)
	require.Equal(t, uint64(4), marketSellsWithLiquidation[2].Id)

	marketSellsWithoutLiquidation := stateOne.GetBlockOrders(
		ctx, utils.ContractAddress(TEST_CONTRACT), utils.PairString(TEST_PAIR)).GetSortedMarketOrders(
		types.PositionDirection_SHORT, false,
	)
	require.Equal(t, uint64(6), marketSellsWithoutLiquidation[0].Id)
	require.Equal(t, uint64(5), marketSellsWithoutLiquidation[1].Id)
}

func TestGetTriggeredOrders(t *testing.T) {
	keeper, ctx := keepertest.DexKeeper(t)
	stateOne := dex.NewMemState(keeper.GetStoreKey())
	stateOne.GetBlockOrders(ctx, utils.ContractAddress(TEST_CONTRACT), utils.PairString(TEST_PAIR)).Add(&types.Order{
		Id:                1,
		Account:           "test",
		ContractAddr:      TEST_CONTRACT,
		PositionDirection: types.PositionDirection_LONG,
		OrderType:         types.OrderType_STOPLOSS,
		Price:             sdk.MustNewDecFromStr("150"),
		TriggerPrice:      sdk.MustNewDecFromStr("100"),
		TriggerStatus:     false,
	})
	stateOne.GetBlockOrders(ctx, utils.ContractAddress(TEST_CONTRACT), utils.PairString(TEST_PAIR)).Add(&types.Order{
		Id:                2,
		Account:           "test",
		ContractAddr:      TEST_CONTRACT,
		PositionDirection: types.PositionDirection_SHORT,
		OrderType:         types.OrderType_STOPLIMIT,
		Price:             sdk.MustNewDecFromStr("150"),
		TriggerPrice:      sdk.MustNewDecFromStr("200"),
		TriggerStatus:     false,
	})
	stateOne.GetBlockOrders(ctx, utils.ContractAddress(TEST_CONTRACT), utils.PairString(TEST_PAIR)).Add(&types.Order{
		Id:                3,
		Account:           "test",
		ContractAddr:      TEST_CONTRACT,
		PositionDirection: types.PositionDirection_LONG,
		OrderType:         types.OrderType_LIMIT,
		Price:             sdk.MustNewDecFromStr("100"),
	})

	triggeredOrders := stateOne.GetBlockOrders(ctx, utils.ContractAddress(TEST_CONTRACT), utils.PairString(TEST_PAIR)).GetTriggeredOrders()
	var orderIds []uint64
	for _, order := range triggeredOrders {
		orderIds = append(orderIds, order.Id)
	}

	require.Equal(t, len(triggeredOrders), 2)
	require.ElementsMatch(t, orderIds, []uint64{1, 2})
}
