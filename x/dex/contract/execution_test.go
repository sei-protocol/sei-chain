package contract_test

import (
	"testing"
	"time"

	"github.com/sei-protocol/sei-chain/utils/datastructures"
	dexutils "github.com/sei-protocol/sei-chain/x/dex/utils"

	sdk "github.com/cosmos/cosmos-sdk/types"
	keepertest "github.com/sei-protocol/sei-chain/testutil/keeper"
	"github.com/sei-protocol/sei-chain/x/dex/contract"
	"github.com/sei-protocol/sei-chain/x/dex/exchange"
	"github.com/sei-protocol/sei-chain/x/dex/types"
	"github.com/sei-protocol/sei-chain/x/dex/types/utils"
	dextypesutils "github.com/sei-protocol/sei-chain/x/dex/types/utils"
	"github.com/stretchr/testify/require"
)

func TEST_PAIR() types.Pair {
	return types.Pair{
		PriceDenom: "usdc",
		AssetDenom: "atom",
	}
}

const (
	TEST_ACCOUNT         = "test_account"
	TEST_CONTRACT        = "test"
	TestTimestamp uint64 = 10000
	TestHeight    uint64 = 1
)

func TestExecutePair(t *testing.T) {
	pair := types.Pair{
		PriceDenom: "USDC",
		AssetDenom: "ATOM",
	}
	dexkeeper, ctx := keepertest.DexKeeper(t)
	ctx = ctx.WithBlockHeight(int64(TestHeight)).WithBlockTime(time.Unix(int64(TestTimestamp), 0))
	longBook := []types.OrderBookEntry{
		&types.LongBook{
			Price: sdk.NewDec(98),
			Entry: &types.OrderEntry{
				Price:    sdk.NewDec(98),
				Quantity: sdk.NewDec(5),
				Allocations: []*types.Allocation{{
					OrderId:  5,
					Account:  "abc",
					Quantity: sdk.NewDec(5),
				}},
				PriceDenom: "USDC",
				AssetDenom: "ATOM",
			},
		},
		&types.LongBook{
			Price: sdk.NewDec(100),
			Entry: &types.OrderEntry{
				Price:    sdk.NewDec(100),
				Quantity: sdk.NewDec(3),
				Allocations: []*types.Allocation{{
					OrderId:  6,
					Account:  "def",
					Quantity: sdk.NewDec(3),
				}},
				PriceDenom: "USDC",
				AssetDenom: "ATOM",
			},
		},
	}
	shortBook := []types.OrderBookEntry{
		&types.ShortBook{
			Price: sdk.NewDec(101),
			Entry: &types.OrderEntry{
				Price:    sdk.NewDec(101),
				Quantity: sdk.NewDec(5),
				Allocations: []*types.Allocation{{
					OrderId:  7,
					Account:  "abc",
					Quantity: sdk.NewDec(5),
				}},
				PriceDenom: "USDC",
				AssetDenom: "ATOM",
			},
		},
		&types.ShortBook{
			Price: sdk.NewDec(115),
			Entry: &types.OrderEntry{
				Price:    sdk.NewDec(115),
				Quantity: sdk.NewDec(3),
				Allocations: []*types.Allocation{{
					OrderId:  8,
					Account:  "def",
					Quantity: sdk.NewDec(3),
				}},
				PriceDenom: "USDC",
				AssetDenom: "ATOM",
			},
		},
	}
	orderbook := &types.OrderBook{
		Longs: &types.CachedSortedOrderBookEntries{
			Entries:      longBook,
			DirtyEntries: datastructures.NewTypedSyncMap[string, types.OrderBookEntry](),
		},
		Shorts: &types.CachedSortedOrderBookEntries{
			Entries:      shortBook,
			DirtyEntries: datastructures.NewTypedSyncMap[string, types.OrderBookEntry](),
		},
	}

	settlements := contract.ExecutePair(
		ctx,
		TEST_CONTRACT,
		pair,
		dexkeeper,
		orderbook,
	)
	require.Equal(t, len(settlements), 0)

	// add Market orders to the orderbook
	dexutils.GetMemState(ctx.Context()).GetBlockOrders(ctx, utils.ContractAddress(TEST_CONTRACT), utils.GetPairString(&pair)).Add(
		&types.Order{
			Id:                1,
			Account:           TEST_ACCOUNT,
			ContractAddr:      TEST_CONTRACT,
			Price:             sdk.MustNewDecFromStr("97"),
			Quantity:          sdk.MustNewDecFromStr("1"),
			PriceDenom:        pair.PriceDenom,
			AssetDenom:        pair.AssetDenom,
			OrderType:         types.OrderType_LIMIT,
			PositionDirection: types.PositionDirection_LONG,
			Data:              "{\"position_effect\":\"Open\",\"leverage\":\"1\"}",
		},
	)
	dexutils.GetMemState(ctx.Context()).GetBlockOrders(ctx, utils.ContractAddress(TEST_CONTRACT), utils.GetPairString(&pair)).Add(
		&types.Order{
			Id:                2,
			Account:           TEST_ACCOUNT,
			ContractAddr:      TEST_CONTRACT,
			Price:             sdk.MustNewDecFromStr("100"),
			Quantity:          sdk.MustNewDecFromStr("1"),
			PriceDenom:        pair.PriceDenom,
			AssetDenom:        pair.AssetDenom,
			OrderType:         types.OrderType_LIMIT,
			PositionDirection: types.PositionDirection_LONG,
			Data:              "{\"position_effect\":\"Open\",\"leverage\":\"1\"}",
		},
	)
	dexutils.GetMemState(ctx.Context()).GetBlockOrders(ctx, utils.ContractAddress(TEST_CONTRACT), utils.GetPairString(&pair)).Add(
		&types.Order{
			Id:                3,
			Account:           TEST_ACCOUNT,
			ContractAddr:      TEST_CONTRACT,
			Price:             sdk.MustNewDecFromStr("200"),
			Quantity:          sdk.MustNewDecFromStr("1"),
			PriceDenom:        pair.PriceDenom,
			AssetDenom:        pair.AssetDenom,
			OrderType:         types.OrderType_MARKET,
			PositionDirection: types.PositionDirection_LONG,
			Data:              "{\"position_effect\":\"Open\",\"leverage\":\"1\"}",
		},
	)

	settlements = contract.ExecutePair(
		ctx,
		TEST_CONTRACT,
		pair,
		dexkeeper,
		orderbook,
	)

	require.Equal(t, 2, len(settlements))
	require.Equal(t, uint64(7), settlements[0].OrderId)
	require.Equal(t, uint64(3), settlements[1].OrderId)

	// get match results
	matches, cancels := contract.GetMatchResults(
		ctx,
		TEST_CONTRACT,
		utils.GetPairString(&pair),
	)
	require.Equal(t, 3, len(matches))
	require.Equal(t, 0, len(cancels))
}

func TestExecutePairInParallel(t *testing.T) {
	pair := types.Pair{
		PriceDenom: "USDC",
		AssetDenom: "ATOM",
	}
	dexkeeper, ctx := keepertest.DexKeeper(t)
	ctx = ctx.WithBlockHeight(int64(TestHeight)).WithBlockTime(time.Unix(int64(TestTimestamp), 0))
	longBook := []types.OrderBookEntry{
		&types.LongBook{
			Price: sdk.NewDec(98),
			Entry: &types.OrderEntry{
				Price:    sdk.NewDec(98),
				Quantity: sdk.NewDec(5),
				Allocations: []*types.Allocation{{
					OrderId:  5,
					Account:  "abc",
					Quantity: sdk.NewDec(5),
				}},
				PriceDenom: "USDC",
				AssetDenom: "ATOM",
			},
		},
		&types.LongBook{
			Price: sdk.NewDec(100),
			Entry: &types.OrderEntry{
				Price:    sdk.NewDec(100),
				Quantity: sdk.NewDec(3),
				Allocations: []*types.Allocation{{
					OrderId:  6,
					Account:  "def",
					Quantity: sdk.NewDec(3),
				}},
				PriceDenom: "USDC",
				AssetDenom: "ATOM",
			},
		},
	}
	shortBook := []types.OrderBookEntry{
		&types.ShortBook{
			Price: sdk.NewDec(101),
			Entry: &types.OrderEntry{
				Price:    sdk.NewDec(101),
				Quantity: sdk.NewDec(5),
				Allocations: []*types.Allocation{{
					OrderId:  7,
					Account:  "abc",
					Quantity: sdk.NewDec(5),
				}},
				PriceDenom: "USDC",
				AssetDenom: "ATOM",
			},
		},
		&types.ShortBook{
			Price: sdk.NewDec(115),
			Entry: &types.OrderEntry{
				Price:    sdk.NewDec(115),
				Quantity: sdk.NewDec(3),
				Allocations: []*types.Allocation{{
					OrderId:  8,
					Account:  "def",
					Quantity: sdk.NewDec(3),
				}},
				PriceDenom: "USDC",
				AssetDenom: "ATOM",
			},
		},
	}
	orderbook := &types.OrderBook{
		Longs: &types.CachedSortedOrderBookEntries{
			Entries:      longBook,
			DirtyEntries: datastructures.NewTypedSyncMap[string, types.OrderBookEntry](),
		},
		Shorts: &types.CachedSortedOrderBookEntries{
			Entries:      shortBook,
			DirtyEntries: datastructures.NewTypedSyncMap[string, types.OrderBookEntry](),
		},
	}

	// execute in parallel simple path
	orderbooks := datastructures.NewTypedSyncMap[dextypesutils.PairString, *types.OrderBook]()
	orderbooks.Store(utils.GetPairString(&pair), orderbook)
	settlements, cancels := contract.ExecutePairsInParallel(
		ctx,
		TEST_CONTRACT,
		dexkeeper,
		[]types.Pair{pair},
		orderbooks,
	)

	require.Equal(t, len(settlements), 0)
	require.Equal(t, len(cancels), 0)

	// add Market orders to the orderbook
	dexutils.GetMemState(ctx.Context()).GetBlockOrders(ctx, utils.ContractAddress(TEST_CONTRACT), utils.GetPairString(&pair)).Add(
		&types.Order{
			Id:                1,
			Account:           TEST_ACCOUNT,
			ContractAddr:      TEST_CONTRACT,
			Price:             sdk.MustNewDecFromStr("97"),
			Quantity:          sdk.MustNewDecFromStr("1"),
			PriceDenom:        pair.PriceDenom,
			AssetDenom:        pair.AssetDenom,
			OrderType:         types.OrderType_LIMIT,
			PositionDirection: types.PositionDirection_LONG,
			Data:              "{\"position_effect\":\"Open\",\"leverage\":\"1\"}",
		},
	)
	dexutils.GetMemState(ctx.Context()).GetBlockOrders(ctx, utils.ContractAddress(TEST_CONTRACT), utils.GetPairString(&pair)).Add(
		&types.Order{
			Id:                2,
			Account:           TEST_ACCOUNT,
			ContractAddr:      TEST_CONTRACT,
			Price:             sdk.MustNewDecFromStr("100"),
			Quantity:          sdk.MustNewDecFromStr("1"),
			PriceDenom:        pair.PriceDenom,
			AssetDenom:        pair.AssetDenom,
			OrderType:         types.OrderType_LIMIT,
			PositionDirection: types.PositionDirection_LONG,
			Data:              "{\"position_effect\":\"Open\",\"leverage\":\"1\"}",
		},
	)
	dexutils.GetMemState(ctx.Context()).GetBlockOrders(ctx, utils.ContractAddress(TEST_CONTRACT), utils.GetPairString(&pair)).Add(
		&types.Order{
			Id:                3,
			Account:           TEST_ACCOUNT,
			ContractAddr:      TEST_CONTRACT,
			Price:             sdk.MustNewDecFromStr("200"),
			Quantity:          sdk.MustNewDecFromStr("1"),
			PriceDenom:        pair.PriceDenom,
			AssetDenom:        pair.AssetDenom,
			OrderType:         types.OrderType_MARKET,
			PositionDirection: types.PositionDirection_LONG,
			Data:              "{\"position_effect\":\"Open\",\"leverage\":\"1\"}",
		},
	)
	dexutils.GetMemState(ctx.Context()).GetBlockOrders(ctx, utils.ContractAddress(TEST_CONTRACT), utils.GetPairString(&pair)).Add(
		&types.Order{
			Id:                11,
			Account:           TEST_ACCOUNT,
			ContractAddr:      TEST_CONTRACT,
			Price:             sdk.MustNewDecFromStr("20"),
			Quantity:          sdk.MustNewDecFromStr("1"),
			PriceDenom:        pair.PriceDenom,
			AssetDenom:        pair.AssetDenom,
			OrderType:         types.OrderType_MARKET,
			PositionDirection: types.PositionDirection_LONG,
			Data:              "{\"position_effect\":\"Open\",\"leverage\":\"1\"}",
		},
	)

	settlements, cancels = contract.ExecutePairsInParallel(
		ctx,
		TEST_CONTRACT,
		dexkeeper,
		[]types.Pair{pair},
		orderbooks,
	)

	require.Equal(t, 2, len(settlements))
	require.Equal(t, 1, len(cancels))
	require.Equal(t, uint64(7), settlements[0].OrderId)
	require.Equal(t, uint64(3), settlements[1].OrderId)
}

func TestGetOrderIDToSettledQuantities(t *testing.T) {
	settlements := []*types.SettlementEntry{
		{
			OrderId:  1,
			Quantity: sdk.MustNewDecFromStr("100"),
		},
		{
			OrderId:  2,
			Quantity: sdk.MustNewDecFromStr("200"),
		},
	}

	idMapping := contract.GetOrderIDToSettledQuantities(settlements)

	require.Equal(t, 2, len(idMapping))
	require.Equal(t, sdk.MustNewDecFromStr("100"), idMapping[1])
	require.Equal(t, sdk.MustNewDecFromStr("200"), idMapping[2])
}

func TestEmitSettlementMetrics(t *testing.T) {
	settlements := []*types.SettlementEntry{
		{
			OrderId:  1,
			Quantity: sdk.MustNewDecFromStr("100"),
		},
		{
			OrderId:  2,
			Quantity: sdk.MustNewDecFromStr("200"),
		},
	}

	contract.EmitSettlementMetrics(settlements)
}

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

	dexkeeper.SetTriggeredOrder(ctx, TEST_CONTRACT, triggeredOrder, TEST_PAIR().PriceDenom, TEST_PAIR().AssetDenom)
	contract.MoveTriggeredOrderForPair(
		ctx,
		utils.ContractAddress(TEST_CONTRACT),
		utils.GetPairString(&pair),
		dexkeeper,
	)
	orders := dexutils.GetMemState(ctx.Context()).GetBlockOrders(ctx, TEST_CONTRACT, utils.GetPairString(&pair))
	cacheMarketOrders := orders.GetSortedMarketOrders(types.PositionDirection_LONG, false)
	cacheTriggeredOrders := orders.GetTriggeredOrders()

	triggeredBookOrders := dexkeeper.GetAllTriggeredOrdersForPair(ctx, TEST_CONTRACT, TEST_PAIR().PriceDenom, TEST_PAIR().AssetDenom)

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

	dexkeeper.SetTriggeredOrder(ctx, TEST_CONTRACT, shortTriggeredOrder, TEST_PAIR().PriceDenom, TEST_PAIR().AssetDenom)
	dexkeeper.SetTriggeredOrder(ctx, TEST_CONTRACT, longNotTriggeredOrder, TEST_PAIR().PriceDenom, TEST_PAIR().AssetDenom)
	orders := dexutils.GetMemState(ctx.Context()).GetBlockOrders(ctx, TEST_CONTRACT, utils.GetPairString(&pair))
	orders.Add(&shortNotTriggeredOrder)
	orders.Add(&longTriggeredOrder)

	contract.UpdateTriggeredOrderForPair(
		ctx,
		utils.ContractAddress(TEST_CONTRACT),
		utils.GetPairString(&pair),
		dexkeeper,
		totalOutcome,
	)

	triggeredBookOrders := dexkeeper.GetAllTriggeredOrdersForPair(ctx, TEST_CONTRACT, TEST_PAIR().PriceDenom, TEST_PAIR().AssetDenom)

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
