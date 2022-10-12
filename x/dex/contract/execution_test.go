package contract_test

import (
	"testing"
	"time"

	sdk "github.com/cosmos/cosmos-sdk/types"
	keepertest "github.com/sei-protocol/sei-chain/testutil/keeper"
	"github.com/sei-protocol/sei-chain/x/dex/contract"
	"github.com/sei-protocol/sei-chain/x/dex/exchange"
	"github.com/sei-protocol/sei-chain/x/dex/types"
	"github.com/sei-protocol/sei-chain/x/dex/types/utils"
	"github.com/stretchr/testify/require"
)

func TEST_PAIR() types.Pair {
	return types.Pair{
		PriceDenom: "usdc",
		AssetDenom: "atom",
	}
}

const (
	TEST_CONTRACT        = "test"
	TestTimestamp uint64 = 10000
	TestHeight    uint64 = 1
)

func TestMoveTriggeredOrderIntoMemState(t *testing.T) {
	pair := TEST_PAIR()
	dexkeeper, ctx := keepertest.DexKeeper(t)
	ctx = ctx.WithBlockHeight(int64(TestHeight)).WithBlockTime(time.Unix(int64(TestTimestamp), 0))
	triggeredOrder := types.Order{
		Id:                8,
		Price:             sdk.MustNewDecFromStr("20"),
		Quantity:          sdk.MustNewDecFromStr("5"),
		Data:              "",
		PositionDirection: types.PositionDirection_LONG,
		OrderType:         types.OrderType_STOPLOSS,
		PriceDenom:        TEST_PAIR().PriceDenom,
		AssetDenom:        TEST_PAIR().AssetDenom,
		TriggerPrice:      sdk.MustNewDecFromStr("10"),
		TriggerStatus:     true,
	}

	dexkeeper.SetTriggerBookOrder(ctx, TEST_CONTRACT, triggeredOrder, TEST_PAIR().PriceDenom, TEST_PAIR().AssetDenom)
	contract.MoveTriggeredOrderForPair(
		ctx,
		utils.ContractAddress(TEST_CONTRACT),
		utils.GetPairString(&pair),
		dexkeeper,
	)
	orders := dexkeeper.MemState.GetBlockOrders(ctx, TEST_CONTRACT, utils.GetPairString(&pair))
	cacheMarketOrders := orders.GetSortedMarketOrders(types.PositionDirection_LONG, false)
	cacheTriggeredOrders := orders.GetTriggeredOrders()

	triggeredBookOrders := dexkeeper.GetAllTriggerBookOrdersForPair(ctx, TEST_CONTRACT, TEST_PAIR().PriceDenom, TEST_PAIR().AssetDenom)

	require.Equal(t, len(triggeredBookOrders), 0)
	require.Equal(t, len(cacheTriggeredOrders), 0)
	require.Equal(t, len(cacheMarketOrders), 1)
	require.Equal(t, cacheMarketOrders[0].Id, uint64(8))
	require.Equal(t, cacheMarketOrders[0].OrderType, types.OrderType_MARKET)
	require.Equal(t, cacheMarketOrders[0].PositionDirection, types.PositionDirection_LONG)
}

func TestUpdateTriggeredOrders(t *testing.T) {
	pair := TEST_PAIR()
	dexkeeper, ctx := keepertest.DexKeeper(t)
	ctx = ctx.WithBlockHeight(int64(TestHeight)).WithBlockTime(time.Unix(int64(TestTimestamp), 0))
	shortTriggeredOrder := types.Order{
		Id:                1,
		Price:             sdk.MustNewDecFromStr("20"),
		Quantity:          sdk.MustNewDecFromStr("5"),
		Data:              "",
		PositionDirection: types.PositionDirection_SHORT,
		OrderType:         types.OrderType_STOPLIMIT,
		PriceDenom:        TEST_PAIR().PriceDenom,
		AssetDenom:        TEST_PAIR().AssetDenom,
		TriggerPrice:      sdk.MustNewDecFromStr("6"),
		TriggerStatus:     false,
	}
	longTriggeredOrder := types.Order{
		Id:                2,
		Price:             sdk.MustNewDecFromStr("20"),
		Quantity:          sdk.MustNewDecFromStr("5"),
		Data:              "",
		PositionDirection: types.PositionDirection_LONG,
		OrderType:         types.OrderType_STOPLOSS,
		PriceDenom:        TEST_PAIR().PriceDenom,
		AssetDenom:        TEST_PAIR().AssetDenom,
		TriggerPrice:      sdk.MustNewDecFromStr("19"),
		TriggerStatus:     false,
	}
	shortNotTriggeredOrder := types.Order{
		Id:                3,
		Price:             sdk.MustNewDecFromStr("20"),
		Quantity:          sdk.MustNewDecFromStr("5"),
		Data:              "",
		PositionDirection: types.PositionDirection_SHORT,
		OrderType:         types.OrderType_STOPLOSS,
		PriceDenom:        TEST_PAIR().PriceDenom,
		AssetDenom:        TEST_PAIR().AssetDenom,
		TriggerPrice:      sdk.MustNewDecFromStr("4"),
		TriggerStatus:     false,
	}
	longNotTriggeredOrder := types.Order{
		Id:                4,
		Price:             sdk.MustNewDecFromStr("20"),
		Quantity:          sdk.MustNewDecFromStr("5"),
		Data:              "",
		PositionDirection: types.PositionDirection_LONG,
		OrderType:         types.OrderType_STOPLIMIT,
		PriceDenom:        TEST_PAIR().PriceDenom,
		AssetDenom:        TEST_PAIR().AssetDenom,
		TriggerPrice:      sdk.MustNewDecFromStr("21"),
		TriggerStatus:     false,
	}

	totalOutcome := exchange.ExecutionOutcome{
		TotalNotional: sdk.MustNewDecFromStr("10"),
		TotalQuantity: sdk.MustNewDecFromStr("10"),
		Settlements:   []*types.SettlementEntry{},
		MinPrice:      sdk.MustNewDecFromStr("5"),
		MaxPrice:      sdk.MustNewDecFromStr("20"),
	}

	dexkeeper.SetTriggerBookOrder(ctx, TEST_CONTRACT, shortTriggeredOrder, TEST_PAIR().PriceDenom, TEST_PAIR().AssetDenom)
	dexkeeper.SetTriggerBookOrder(ctx, TEST_CONTRACT, longNotTriggeredOrder, TEST_PAIR().PriceDenom, TEST_PAIR().AssetDenom)
	orders := dexkeeper.MemState.GetBlockOrders(ctx, TEST_CONTRACT, utils.GetPairString(&pair))
	orders.Add(&shortNotTriggeredOrder)
	orders.Add(&longTriggeredOrder)

	contract.UpdateTriggeredOrderForPair(
		ctx,
		utils.ContractAddress(TEST_CONTRACT),
		utils.GetPairString(&pair),
		dexkeeper,
		totalOutcome,
	)

	triggeredBookOrders := dexkeeper.GetAllTriggerBookOrdersForPair(ctx, TEST_CONTRACT, TEST_PAIR().PriceDenom, TEST_PAIR().AssetDenom)

	require.Equal(t, len(triggeredBookOrders), 4)
	triggerStatusMap := map[uint64]bool{}

	for _, order := range triggeredBookOrders {
		triggerStatusMap[order.Id] = order.TriggerStatus
	}
	require.Contains(t, triggerStatusMap, uint64(1))
	require.Contains(t, triggerStatusMap, uint64(2))
	require.Contains(t, triggerStatusMap, uint64(3))
	require.Contains(t, triggerStatusMap, uint64(4))

	require.Equal(t, triggerStatusMap[uint64(1)], true)
	require.Equal(t, triggerStatusMap[uint64(2)], true)
	require.Equal(t, triggerStatusMap[uint64(3)], false)
	require.Equal(t, triggerStatusMap[uint64(4)], false)
}
