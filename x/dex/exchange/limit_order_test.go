package exchange_test

import (
	"testing"
	"time"

	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/sei-protocol/sei-chain/x/dex/exchange"
	keeperutil "github.com/sei-protocol/sei-chain/x/dex/keeper/utils"
	"github.com/sei-protocol/sei-chain/x/dex/types"
	"github.com/stretchr/testify/assert"

	keepertest "github.com/sei-protocol/sei-chain/testutil/keeper"
)

func TEST_PAIR() types.Pair {
	return types.Pair{
		PriceDenom: "usdc",
		AssetDenom: "atom",
	}
}

func TestMatchSingleOrder(t *testing.T) {
	dexkeeper, ctx := keepertest.DexKeeper(t)
	ctx = ctx.WithBlockHeight(int64(TestHeight)).WithBlockTime(time.Unix(int64(TestTimestamp), 0))
	longOrders := []*types.Order{
		{
			Id:                1,
			Price:             sdk.NewDec(100),
			Quantity:          sdk.NewDec(5),
			Account:           "abc",
			PositionDirection: types.PositionDirection_LONG,
			ContractAddr:      "test",
			PriceDenom:        "USDC",
			AssetDenom:        "ATOM",
			OrderType:         types.OrderType_LIMIT,
		},
	}
	shortOrders := []*types.Order{
		{
			Id:                2,
			Price:             sdk.NewDec(100),
			Quantity:          sdk.NewDec(5),
			Account:           "def",
			PositionDirection: types.PositionDirection_SHORT,
			ContractAddr:      "test",
			PriceDenom:        "USDC",
			AssetDenom:        "ATOM",
			OrderType:         types.OrderType_LIMIT,
		},
	}
	longBook := []types.OrderBookEntry{}
	shortBook := []types.OrderBookEntry{}
	exchange.AddOutstandingLimitOrdersToOrderbook(ctx, dexkeeper, longOrders, shortOrders)
	orderbook := keeperutil.PopulateOrderbook(ctx, dexkeeper, types.ContractAddress("test"), types.Pair{PriceDenom: "USDC", AssetDenom: "ATOM"})
	outcome := exchange.MatchLimitOrders(
		ctx, orderbook,
	)
	totalPrice := outcome.TotalNotional
	totalExecuted := outcome.TotalQuantity
	settlements := outcome.Settlements
	assert.Equal(t, totalPrice, sdk.NewDec(1000))
	assert.Equal(t, totalExecuted, sdk.NewDec(10))
	assert.Equal(t, outcome.MaxPrice, sdk.NewDec(100))
	assert.Equal(t, outcome.MinPrice, sdk.NewDec(100))
	longBook = dexkeeper.GetAllLongBookForPair(ctx, "test", "USDC", "ATOM")
	shortBook = dexkeeper.GetAllShortBookForPair(ctx, "test", "USDC", "ATOM")
	assert.Equal(t, len(longBook), 0)
	assert.Equal(t, len(shortBook), 0)
	assert.Equal(t, len(settlements), 2)
	assert.Equal(t, *settlements[0], types.SettlementEntry{
		PositionDirection:      "Long",
		PriceDenom:             "USDC",
		AssetDenom:             "ATOM",
		Quantity:               sdk.NewDec(5),
		ExecutionCostOrProceed: sdk.NewDec(100),
		ExpectedCostOrProceed:  sdk.NewDec(100),
		Account:                "abc",
		OrderType:              "Limit",
		OrderId:                1,
		Timestamp:              TestTimestamp,
		Height:                 TestHeight,
	})
	assert.Equal(t, *settlements[1], types.SettlementEntry{
		PositionDirection:      "Short",
		PriceDenom:             "USDC",
		AssetDenom:             "ATOM",
		Quantity:               sdk.NewDec(5),
		ExecutionCostOrProceed: sdk.NewDec(100),
		ExpectedCostOrProceed:  sdk.NewDec(100),
		Account:                "def",
		OrderType:              "Limit",
		OrderId:                2,
		Timestamp:              TestTimestamp,
		Height:                 TestHeight,
	})
}

func TestAddOrders(t *testing.T) {
	dexkeeper, ctx := keepertest.DexKeeper(t)
	ctx = ctx.WithBlockHeight(int64(TestHeight)).WithBlockTime(time.Unix(int64(TestTimestamp), 0))
	longOrders := []*types.Order{
		{
			Id:                1,
			Price:             sdk.NewDec(100),
			Quantity:          sdk.NewDec(5),
			Account:           "def",
			PositionDirection: types.PositionDirection_LONG,
			ContractAddr:      "test",
			PriceDenom:        "USDC",
			AssetDenom:        "ATOM",
			OrderType:         types.OrderType_LIMIT,
		},
		{
			Id:                2,
			Price:             sdk.NewDec(95),
			Quantity:          sdk.NewDec(3),
			Account:           "def",
			PositionDirection: types.PositionDirection_LONG,
			ContractAddr:      "test",
			PriceDenom:        "USDC",
			AssetDenom:        "ATOM",
			OrderType:         types.OrderType_LIMIT,
		},
	}
	shortOrders := []*types.Order{
		{
			Id:                3,
			Price:             sdk.NewDec(105),
			Quantity:          sdk.NewDec(10),
			Account:           "ghi",
			PositionDirection: types.PositionDirection_SHORT,
			ContractAddr:      "test",
			PriceDenom:        "USDC",
			AssetDenom:        "ATOM",
			OrderType:         types.OrderType_LIMIT,
		},
		{
			Id:                4,
			Price:             sdk.NewDec(115),
			Quantity:          sdk.NewDec(2),
			Account:           "mno",
			PositionDirection: types.PositionDirection_SHORT,
			ContractAddr:      "test",
			PriceDenom:        "USDC",
			AssetDenom:        "ATOM",
			OrderType:         types.OrderType_LIMIT,
		},
	}
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
	for _, e := range longBook {
		dexkeeper.SetLongOrderBookEntry(ctx, "test", e)
	}
	for _, e := range shortBook {
		dexkeeper.SetShortOrderBookEntry(ctx, "test", e)
	}
	exchange.AddOutstandingLimitOrdersToOrderbook(ctx, dexkeeper, longOrders, shortOrders)
	orderbook := keeperutil.PopulateOrderbook(ctx, dexkeeper, types.ContractAddress("test"), types.Pair{PriceDenom: "USDC", AssetDenom: "ATOM"})
	outcome := exchange.MatchLimitOrders(
		ctx, orderbook,
	)
	totalPrice := outcome.TotalNotional
	totalExecuted := outcome.TotalQuantity
	settlements := outcome.Settlements
	assert.Equal(t, totalPrice, sdk.NewDec(0))
	assert.Equal(t, totalExecuted, sdk.NewDec(0))
	longBook = dexkeeper.GetAllLongBookForPair(ctx, "test", "USDC", "ATOM")
	shortBook = dexkeeper.GetAllShortBookForPair(ctx, "test", "USDC", "ATOM")
	assert.Equal(t, len(longBook), 3)
	assert.Equal(t, longBook[0].GetPrice(), sdk.NewDec(95))
	assert.Equal(t, *longBook[0].GetOrderEntry(), types.OrderEntry{
		Price:    sdk.NewDec(95),
		Quantity: sdk.NewDec(3),
		Allocations: []*types.Allocation{{
			OrderId:  2,
			Account:  "def",
			Quantity: sdk.NewDec(3),
		}},
		PriceDenom: "USDC",
		AssetDenom: "ATOM",
	})
	assert.Equal(t, longBook[1].GetPrice(), sdk.NewDec(98))
	assert.Equal(t, *longBook[1].GetOrderEntry(), types.OrderEntry{
		Price:    sdk.NewDec(98),
		Quantity: sdk.NewDec(5),
		Allocations: []*types.Allocation{{
			OrderId:  5,
			Account:  "abc",
			Quantity: sdk.NewDec(5),
		}},
		PriceDenom: "USDC",
		AssetDenom: "ATOM",
	})
	assert.Equal(t, longBook[2].GetPrice(), sdk.NewDec(100))
	assert.Equal(t, *longBook[2].GetOrderEntry(), types.OrderEntry{
		Price:    sdk.NewDec(100),
		Quantity: sdk.NewDec(8),
		Allocations: []*types.Allocation{{
			OrderId:  6,
			Account:  "def",
			Quantity: sdk.NewDec(3),
		}, {
			OrderId:  1,
			Account:  "def",
			Quantity: sdk.NewDec(5),
		}},
		PriceDenom: "USDC",
		AssetDenom: "ATOM",
	})
	assert.Equal(t, len(shortBook), 3)
	assert.Equal(t, shortBook[0].GetPrice(), sdk.NewDec(101))
	assert.Equal(t, *shortBook[0].GetOrderEntry(), types.OrderEntry{
		Price:    sdk.NewDec(101),
		Quantity: sdk.NewDec(5),
		Allocations: []*types.Allocation{{
			OrderId:  7,
			Account:  "abc",
			Quantity: sdk.NewDec(5),
		}},
		PriceDenom: "USDC",
		AssetDenom: "ATOM",
	})
	assert.Equal(t, shortBook[1].GetPrice(), sdk.NewDec(105))
	assert.Equal(t, *shortBook[1].GetOrderEntry(), types.OrderEntry{
		Price:    sdk.NewDec(105),
		Quantity: sdk.NewDec(10),
		Allocations: []*types.Allocation{{
			OrderId:  3,
			Account:  "ghi",
			Quantity: sdk.NewDec(10),
		}},
		PriceDenom: "USDC",
		AssetDenom: "ATOM",
	})
	assert.Equal(t, shortBook[2].GetPrice(), sdk.NewDec(115))
	assert.Equal(t, *shortBook[2].GetOrderEntry(), types.OrderEntry{
		Price:    sdk.NewDec(115),
		Quantity: sdk.NewDec(5),
		Allocations: []*types.Allocation{{
			OrderId:  8,
			Account:  "def",
			Quantity: sdk.NewDec(3),
		}, {
			OrderId:  4,
			Account:  "mno",
			Quantity: sdk.NewDec(2),
		}},
		PriceDenom: "USDC",
		AssetDenom: "ATOM",
	})
	assert.Equal(t, len(settlements), 0)
}

func TestMatchSingleOrderFromShortBook(t *testing.T) {
	dexkeeper, ctx := keepertest.DexKeeper(t)
	ctx = ctx.WithBlockHeight(int64(TestHeight)).WithBlockTime(time.Unix(int64(TestTimestamp), 0))
	longOrders := []*types.Order{
		{
			Id:                1,
			Price:             sdk.NewDec(100),
			Quantity:          sdk.NewDec(5),
			Account:           "abc",
			PositionDirection: types.PositionDirection_LONG,
			ContractAddr:      "test",
			PriceDenom:        "USDC",
			AssetDenom:        "ATOM",
			OrderType:         types.OrderType_LIMIT,
		},
	}
	shortOrders := []*types.Order{}
	longBook := []types.OrderBookEntry{}
	shortBook := []types.OrderBookEntry{
		&types.ShortBook{
			Price: sdk.NewDec(100),
			Entry: &types.OrderEntry{
				Price:    sdk.NewDec(100),
				Quantity: sdk.NewDec(5),
				Allocations: []*types.Allocation{{
					OrderId:  2,
					Account:  "def",
					Quantity: sdk.NewDec(5),
				}},
				PriceDenom: "USDC",
				AssetDenom: "ATOM",
			},
		},
	}
	for _, e := range longBook {
		dexkeeper.SetLongOrderBookEntry(ctx, "test", e)
	}
	for _, e := range shortBook {
		dexkeeper.SetShortOrderBookEntry(ctx, "test", e)
	}
	exchange.AddOutstandingLimitOrdersToOrderbook(ctx, dexkeeper, longOrders, shortOrders)
	orderbook := keeperutil.PopulateOrderbook(ctx, dexkeeper, types.ContractAddress("test"), types.Pair{PriceDenom: "USDC", AssetDenom: "ATOM"})
	outcome := exchange.MatchLimitOrders(
		ctx, orderbook,
	)
	totalPrice := outcome.TotalNotional
	totalExecuted := outcome.TotalQuantity
	settlements := outcome.Settlements
	assert.Equal(t, totalPrice, sdk.NewDec(1000))
	assert.Equal(t, totalExecuted, sdk.NewDec(10))
	longBook = dexkeeper.GetAllLongBookForPair(ctx, "test", "USDC", "ATOM")
	shortBook = dexkeeper.GetAllShortBookForPair(ctx, "test", "USDC", "ATOM")
	assert.Equal(t, len(longBook), 0)
	assert.Equal(t, len(shortBook), 0)
	assert.Equal(t, len(settlements), 2)
	assert.Equal(t, *settlements[0], types.SettlementEntry{
		PositionDirection:      "Long",
		PriceDenom:             "USDC",
		AssetDenom:             "ATOM",
		Quantity:               sdk.NewDec(5),
		ExecutionCostOrProceed: sdk.NewDec(100),
		ExpectedCostOrProceed:  sdk.NewDec(100),
		Account:                "abc",
		OrderType:              "Limit",
		OrderId:                1,
		Timestamp:              TestTimestamp,
		Height:                 TestHeight,
	})
	assert.Equal(t, *settlements[1], types.SettlementEntry{
		PositionDirection:      "Short",
		PriceDenom:             "USDC",
		AssetDenom:             "ATOM",
		Quantity:               sdk.NewDec(5),
		ExecutionCostOrProceed: sdk.NewDec(100),
		ExpectedCostOrProceed:  sdk.NewDec(100),
		Account:                "def",
		OrderType:              "Limit",
		OrderId:                2,
		Timestamp:              TestTimestamp,
		Height:                 TestHeight,
	})
}

func TestMatchSingleOrderFromLongBook(t *testing.T) {
	dexkeeper, ctx := keepertest.DexKeeper(t)
	ctx = ctx.WithBlockHeight(int64(TestHeight)).WithBlockTime(time.Unix(int64(TestTimestamp), 0))
	longOrders := []*types.Order{}
	shortOrders := []*types.Order{
		{
			Id:                1,
			Price:             sdk.NewDec(100),
			Quantity:          sdk.NewDec(5),
			Account:           "def",
			PositionDirection: types.PositionDirection_SHORT,
			ContractAddr:      "test",
			PriceDenom:        "USDC",
			AssetDenom:        "ATOM",
			OrderType:         types.OrderType_LIMIT,
		},
	}
	longBook := []types.OrderBookEntry{
		&types.LongBook{
			Price: sdk.NewDec(100),
			Entry: &types.OrderEntry{
				Price:    sdk.NewDec(100),
				Quantity: sdk.NewDec(5),
				Allocations: []*types.Allocation{{
					OrderId:  2,
					Account:  "abc",
					Quantity: sdk.NewDec(5),
				}},
				PriceDenom: "USDC",
				AssetDenom: "ATOM",
			},
		},
	}
	shortBook := []types.OrderBookEntry{}
	for _, e := range longBook {
		dexkeeper.SetLongOrderBookEntry(ctx, "test", e)
	}
	for _, e := range shortBook {
		dexkeeper.SetShortOrderBookEntry(ctx, "test", e)
	}
	exchange.AddOutstandingLimitOrdersToOrderbook(ctx, dexkeeper, longOrders, shortOrders)
	orderbook := keeperutil.PopulateOrderbook(ctx, dexkeeper, types.ContractAddress("test"), types.Pair{PriceDenom: "USDC", AssetDenom: "ATOM"})
	outcome := exchange.MatchLimitOrders(
		ctx, orderbook,
	)
	totalPrice := outcome.TotalNotional
	totalExecuted := outcome.TotalQuantity
	settlements := outcome.Settlements
	assert.Equal(t, totalPrice, sdk.NewDec(1000))
	assert.Equal(t, totalExecuted, sdk.NewDec(10))
	longBook = dexkeeper.GetAllLongBookForPair(ctx, "test", "USDC", "ATOM")
	shortBook = dexkeeper.GetAllShortBookForPair(ctx, "test", "USDC", "ATOM")
	assert.Equal(t, len(longBook), 0)
	assert.Equal(t, len(shortBook), 0)
	assert.Equal(t, len(settlements), 2)
	assert.Equal(t, *settlements[0], types.SettlementEntry{
		PositionDirection:      "Long",
		PriceDenom:             "USDC",
		AssetDenom:             "ATOM",
		Quantity:               sdk.NewDec(5),
		ExecutionCostOrProceed: sdk.NewDec(100),
		ExpectedCostOrProceed:  sdk.NewDec(100),
		Account:                "abc",
		OrderType:              "Limit",
		OrderId:                2,
		Timestamp:              TestTimestamp,
		Height:                 TestHeight,
	})
	assert.Equal(t, *settlements[1], types.SettlementEntry{
		PositionDirection:      "Short",
		PriceDenom:             "USDC",
		AssetDenom:             "ATOM",
		Quantity:               sdk.NewDec(5),
		ExecutionCostOrProceed: sdk.NewDec(100),
		ExpectedCostOrProceed:  sdk.NewDec(100),
		Account:                "def",
		OrderType:              "Limit",
		OrderId:                1,
		Timestamp:              TestTimestamp,
		Height:                 TestHeight,
	})
}

func TestMatchSingleOrderFromMultipleShortBook(t *testing.T) {
	dexkeeper, ctx := keepertest.DexKeeper(t)
	ctx = ctx.WithBlockHeight(int64(TestHeight)).WithBlockTime(time.Unix(int64(TestTimestamp), 0))
	longOrders := []*types.Order{
		{
			Id:                1,
			Price:             sdk.NewDec(100),
			Quantity:          sdk.NewDec(5),
			Account:           "abc",
			PositionDirection: types.PositionDirection_LONG,
			ContractAddr:      "test",
			PriceDenom:        "USDC",
			AssetDenom:        "ATOM",
			OrderType:         types.OrderType_LIMIT,
		},
	}
	shortOrders := []*types.Order{}
	longBook := []types.OrderBookEntry{}
	shortBook := []types.OrderBookEntry{
		&types.ShortBook{
			Price: sdk.NewDec(90),
			Entry: &types.OrderEntry{
				Price:    sdk.NewDec(90),
				Quantity: sdk.NewDec(2),
				Allocations: []*types.Allocation{{
					OrderId:  2,
					Account:  "def",
					Quantity: sdk.NewDec(2),
				}},
				PriceDenom: "USDC",
				AssetDenom: "ATOM",
			},
		},
		&types.ShortBook{
			Price: sdk.NewDec(100),
			Entry: &types.OrderEntry{
				Price:    sdk.NewDec(100),
				Quantity: sdk.NewDec(6),
				Allocations: []*types.Allocation{{
					OrderId:  3,
					Account:  "def",
					Quantity: sdk.NewDec(4),
				}, {
					OrderId:  4,
					Account:  "ghi",
					Quantity: sdk.NewDec(2),
				}},
				PriceDenom: "USDC",
				AssetDenom: "ATOM",
			},
		},
	}
	for _, e := range longBook {
		dexkeeper.SetLongOrderBookEntry(ctx, "test", e)
	}
	for _, e := range shortBook {
		dexkeeper.SetShortOrderBookEntry(ctx, "test", e)
	}
	exchange.AddOutstandingLimitOrdersToOrderbook(ctx, dexkeeper, longOrders, shortOrders)
	orderbook := keeperutil.PopulateOrderbook(ctx, dexkeeper, types.ContractAddress("test"), types.Pair{PriceDenom: "USDC", AssetDenom: "ATOM"})
	outcome := exchange.MatchLimitOrders(
		ctx, orderbook,
	)
	totalPrice := outcome.TotalNotional
	totalExecuted := outcome.TotalQuantity
	settlements := outcome.Settlements
	assert.Equal(t, totalPrice, sdk.NewDec(980))
	assert.Equal(t, totalExecuted, sdk.NewDec(10))
	longBook = dexkeeper.GetAllLongBookForPair(ctx, "test", "USDC", "ATOM")
	shortBook = dexkeeper.GetAllShortBookForPair(ctx, "test", "USDC", "ATOM")
	assert.Equal(t, len(longBook), 0)
	assert.Equal(t, len(shortBook), 1)
	assert.Equal(t, shortBook[0].GetPrice(), sdk.NewDec(100))
	assert.Equal(t, *shortBook[0].GetOrderEntry(), types.OrderEntry{
		Price:    sdk.NewDec(100),
		Quantity: sdk.NewDec(3),
		Allocations: []*types.Allocation{{
			OrderId:  3,
			Account:  "def",
			Quantity: sdk.NewDec(1),
		}, {
			OrderId:  4,
			Account:  "ghi",
			Quantity: sdk.NewDec(2),
		}},
		PriceDenom: "USDC",
		AssetDenom: "ATOM",
	})
	assert.Equal(t, len(settlements), 4)
	assert.Equal(t, *settlements[0], types.SettlementEntry{
		PositionDirection:      "Long",
		PriceDenom:             "USDC",
		AssetDenom:             "ATOM",
		Quantity:               sdk.NewDec(2),
		ExecutionCostOrProceed: sdk.NewDec(95),
		ExpectedCostOrProceed:  sdk.NewDec(100),
		Account:                "abc",
		OrderType:              "Limit",
		OrderId:                1,
		Timestamp:              TestTimestamp,
		Height:                 TestHeight,
	})
	assert.Equal(t, *settlements[1], types.SettlementEntry{
		PositionDirection:      "Short",
		PriceDenom:             "USDC",
		AssetDenom:             "ATOM",
		Quantity:               sdk.NewDec(2),
		ExecutionCostOrProceed: sdk.NewDec(95),
		ExpectedCostOrProceed:  sdk.NewDec(90),
		Account:                "def",
		OrderType:              "Limit",
		OrderId:                2,
		Timestamp:              TestTimestamp,
		Height:                 TestHeight,
	})
	assert.Equal(t, *settlements[2], types.SettlementEntry{
		PositionDirection:      "Long",
		PriceDenom:             "USDC",
		AssetDenom:             "ATOM",
		Quantity:               sdk.NewDec(3),
		ExecutionCostOrProceed: sdk.NewDec(100),
		ExpectedCostOrProceed:  sdk.NewDec(100),
		Account:                "abc",
		OrderType:              "Limit",
		OrderId:                1,
		Timestamp:              TestTimestamp,
		Height:                 TestHeight,
	})
	assert.Equal(t, *settlements[3], types.SettlementEntry{
		PositionDirection:      "Short",
		PriceDenom:             "USDC",
		AssetDenom:             "ATOM",
		Quantity:               sdk.NewDec(3),
		ExecutionCostOrProceed: sdk.NewDec(100),
		ExpectedCostOrProceed:  sdk.NewDec(100),
		Account:                "def",
		OrderType:              "Limit",
		OrderId:                3,
		Timestamp:              TestTimestamp,
		Height:                 TestHeight,
	})
}

func TestMatchSingleOrderFromMultipleLongBook(t *testing.T) {
	dexkeeper, ctx := keepertest.DexKeeper(t)
	ctx = ctx.WithBlockHeight(int64(TestHeight)).WithBlockTime(time.Unix(int64(TestTimestamp), 0))
	longOrders := []*types.Order{}
	shortOrders := []*types.Order{
		{
			Id:                1,
			Price:             sdk.NewDec(100),
			Quantity:          sdk.NewDec(5),
			Account:           "def",
			PositionDirection: types.PositionDirection_SHORT,
			ContractAddr:      "test",
			PriceDenom:        "USDC",
			AssetDenom:        "ATOM",
			OrderType:         types.OrderType_LIMIT,
		},
	}
	longBook := []types.OrderBookEntry{
		&types.LongBook{
			Price: sdk.NewDec(100),
			Entry: &types.OrderEntry{
				Price:    sdk.NewDec(100),
				Quantity: sdk.NewDec(6),
				Allocations: []*types.Allocation{{
					OrderId:  2,
					Account:  "abc",
					Quantity: sdk.NewDec(4),
				}, {
					OrderId:  3,
					Account:  "ghi",
					Quantity: sdk.NewDec(2),
				}},
				PriceDenom: "USDC",
				AssetDenom: "ATOM",
			},
		},
		&types.LongBook{
			Price: sdk.NewDec(110),
			Entry: &types.OrderEntry{
				Price:    sdk.NewDec(110),
				Quantity: sdk.NewDec(2),
				Allocations: []*types.Allocation{{
					OrderId:  4,
					Account:  "abc",
					Quantity: sdk.NewDec(2),
				}},
				PriceDenom: "USDC",
				AssetDenom: "ATOM",
			},
		},
	}
	shortBook := []types.OrderBookEntry{}
	for _, e := range longBook {
		dexkeeper.SetLongOrderBookEntry(ctx, "test", e)
	}
	for _, e := range shortBook {
		dexkeeper.SetShortOrderBookEntry(ctx, "test", e)
	}
	exchange.AddOutstandingLimitOrdersToOrderbook(ctx, dexkeeper, longOrders, shortOrders)
	orderbook := keeperutil.PopulateOrderbook(ctx, dexkeeper, types.ContractAddress("test"), types.Pair{PriceDenom: "USDC", AssetDenom: "ATOM"})
	outcome := exchange.MatchLimitOrders(
		ctx, orderbook,
	)
	totalPrice := outcome.TotalNotional
	totalExecuted := outcome.TotalQuantity
	settlements := outcome.Settlements
	assert.Equal(t, totalPrice, sdk.NewDec(1020))
	assert.Equal(t, totalExecuted, sdk.NewDec(10))
	longBook = dexkeeper.GetAllLongBookForPair(ctx, "test", "USDC", "ATOM")
	shortBook = dexkeeper.GetAllShortBookForPair(ctx, "test", "USDC", "ATOM")
	assert.Equal(t, len(longBook), 1)
	assert.Equal(t, longBook[0].GetPrice(), sdk.NewDec(100))
	assert.Equal(t, *longBook[0].GetOrderEntry(), types.OrderEntry{
		Price:    sdk.NewDec(100),
		Quantity: sdk.NewDec(3),
		Allocations: []*types.Allocation{{
			OrderId:  2,
			Account:  "abc",
			Quantity: sdk.NewDec(1),
		}, {
			OrderId:  3,
			Account:  "ghi",
			Quantity: sdk.NewDec(2),
		}},
		PriceDenom: "USDC",
		AssetDenom: "ATOM",
	})
	assert.Equal(t, len(shortBook), 0)
	assert.Equal(t, len(settlements), 4)
	assert.Equal(t, *settlements[0], types.SettlementEntry{
		PositionDirection:      "Long",
		PriceDenom:             "USDC",
		AssetDenom:             "ATOM",
		Quantity:               sdk.NewDec(2),
		ExecutionCostOrProceed: sdk.NewDec(105),
		ExpectedCostOrProceed:  sdk.NewDec(110),
		Account:                "abc",
		OrderType:              "Limit",
		OrderId:                4,
		Timestamp:              TestTimestamp,
		Height:                 TestHeight,
	})
	assert.Equal(t, *settlements[1], types.SettlementEntry{
		PositionDirection:      "Short",
		PriceDenom:             "USDC",
		AssetDenom:             "ATOM",
		Quantity:               sdk.NewDec(2),
		ExecutionCostOrProceed: sdk.NewDec(105),
		ExpectedCostOrProceed:  sdk.NewDec(100),
		Account:                "def",
		OrderType:              "Limit",
		OrderId:                1,
		Timestamp:              TestTimestamp,
		Height:                 TestHeight,
	})
	assert.Equal(t, *settlements[2], types.SettlementEntry{
		PositionDirection:      "Long",
		PriceDenom:             "USDC",
		AssetDenom:             "ATOM",
		Quantity:               sdk.NewDec(3),
		ExecutionCostOrProceed: sdk.NewDec(100),
		ExpectedCostOrProceed:  sdk.NewDec(100),
		Account:                "abc",
		OrderType:              "Limit",
		OrderId:                2,
		Timestamp:              TestTimestamp,
		Height:                 TestHeight,
	})
	assert.Equal(t, *settlements[3], types.SettlementEntry{
		PositionDirection:      "Short",
		PriceDenom:             "USDC",
		AssetDenom:             "ATOM",
		Quantity:               sdk.NewDec(3),
		ExecutionCostOrProceed: sdk.NewDec(100),
		ExpectedCostOrProceed:  sdk.NewDec(100),
		Account:                "def",
		OrderType:              "Limit",
		OrderId:                1,
		Timestamp:              TestTimestamp,
		Height:                 TestHeight,
	})
}

func TestMatchMultipleOrderFromMultipleShortBook(t *testing.T) {
	dexkeeper, ctx := keepertest.DexKeeper(t)
	ctx = ctx.WithBlockHeight(int64(TestHeight)).WithBlockTime(time.Unix(int64(TestTimestamp), 0))
	longOrders := []*types.Order{
		{
			Id:                1,
			Price:             sdk.NewDec(100),
			Quantity:          sdk.NewDec(5),
			Account:           "abc",
			PositionDirection: types.PositionDirection_LONG,
			ContractAddr:      "test",
			PriceDenom:        "USDC",
			AssetDenom:        "ATOM",
			OrderType:         types.OrderType_LIMIT,
		},
		{
			Id:                2,
			Price:             sdk.NewDec(104),
			Quantity:          sdk.NewDec(1),
			Account:           "jkl",
			PositionDirection: types.PositionDirection_LONG,
			ContractAddr:      "test",
			PriceDenom:        "USDC",
			AssetDenom:        "ATOM",
			OrderType:         types.OrderType_LIMIT,
		},
		{
			Id:                3,
			Price:             sdk.NewDec(98),
			Quantity:          sdk.NewDec(2),
			Account:           "mno",
			PositionDirection: types.PositionDirection_LONG,
			ContractAddr:      "test",
			PriceDenom:        "USDC",
			AssetDenom:        "ATOM",
			OrderType:         types.OrderType_LIMIT,
		},
	}
	shortOrders := []*types.Order{}
	longBook := []types.OrderBookEntry{}
	shortBook := []types.OrderBookEntry{
		&types.ShortBook{
			Price: sdk.NewDec(90),
			Entry: &types.OrderEntry{
				Price:    sdk.NewDec(90),
				Quantity: sdk.NewDec(2),
				Allocations: []*types.Allocation{{
					OrderId:  4,
					Account:  "def",
					Quantity: sdk.NewDec(2),
				}},
				PriceDenom: "USDC",
				AssetDenom: "ATOM",
			},
		},
		&types.ShortBook{
			Price: sdk.NewDec(100),
			Entry: &types.OrderEntry{
				Price:    sdk.NewDec(100),
				Quantity: sdk.NewDec(6),
				Allocations: []*types.Allocation{{
					OrderId:  5,
					Account:  "def",
					Quantity: sdk.NewDec(4),
				}, {
					OrderId:  6,
					Account:  "ghi",
					Quantity: sdk.NewDec(2),
				}},
				PriceDenom: "USDC",
				AssetDenom: "ATOM",
			},
		},
	}
	for _, e := range longBook {
		dexkeeper.SetLongOrderBookEntry(ctx, "test", e)
	}
	for _, e := range shortBook {
		dexkeeper.SetShortOrderBookEntry(ctx, "test", e)
	}
	exchange.AddOutstandingLimitOrdersToOrderbook(ctx, dexkeeper, longOrders, shortOrders)
	orderbook := keeperutil.PopulateOrderbook(ctx, dexkeeper, types.ContractAddress("test"), types.Pair{PriceDenom: "USDC", AssetDenom: "ATOM"})
	outcome := exchange.MatchLimitOrders(
		ctx, orderbook,
	)
	totalPrice := outcome.TotalNotional
	totalExecuted := outcome.TotalQuantity
	settlements := outcome.Settlements
	assert.Equal(t, totalPrice, sdk.NewDec(1184))
	assert.Equal(t, totalExecuted, sdk.NewDec(12))
	longBook = dexkeeper.GetAllLongBookForPair(ctx, "test", "USDC", "ATOM")
	shortBook = dexkeeper.GetAllShortBookForPair(ctx, "test", "USDC", "ATOM")
	assert.Equal(t, len(longBook), 1)
	assert.Equal(t, longBook[0].GetPrice(), sdk.NewDec(98))
	assert.Equal(t, *longBook[0].GetOrderEntry(), types.OrderEntry{
		Price:    sdk.NewDec(98),
		Quantity: sdk.NewDec(2),
		Allocations: []*types.Allocation{{
			OrderId:  3,
			Account:  "mno",
			Quantity: sdk.NewDec(2),
		}},
		PriceDenom: "USDC",
		AssetDenom: "ATOM",
	})
	assert.Equal(t, len(shortBook), 1)
	assert.Equal(t, shortBook[0].GetPrice(), sdk.NewDec(100))
	assert.Equal(t, *shortBook[0].GetOrderEntry(), types.OrderEntry{
		Price:    sdk.NewDec(100),
		Quantity: sdk.NewDec(2),
		Allocations: []*types.Allocation{{
			OrderId:  6,
			Account:  "ghi",
			Quantity: sdk.NewDec(2),
		}},
		PriceDenom: "USDC",
		AssetDenom: "ATOM",
	})
	assert.Equal(t, len(settlements), 6)
	assert.Equal(t, *settlements[0], types.SettlementEntry{
		PositionDirection:      "Long",
		PriceDenom:             "USDC",
		AssetDenom:             "ATOM",
		Quantity:               sdk.NewDec(1),
		ExecutionCostOrProceed: sdk.NewDec(97),
		ExpectedCostOrProceed:  sdk.NewDec(104),
		Account:                "jkl",
		OrderType:              "Limit",
		OrderId:                2,
		Timestamp:              TestTimestamp,
		Height:                 TestHeight,
	})
	assert.Equal(t, *settlements[1], types.SettlementEntry{
		PositionDirection:      "Short",
		PriceDenom:             "USDC",
		AssetDenom:             "ATOM",
		Quantity:               sdk.NewDec(1),
		ExecutionCostOrProceed: sdk.NewDec(97),
		ExpectedCostOrProceed:  sdk.NewDec(90),
		Account:                "def",
		OrderType:              "Limit",
		OrderId:                4,
		Timestamp:              TestTimestamp,
		Height:                 TestHeight,
	})
	assert.Equal(t, *settlements[2], types.SettlementEntry{
		PositionDirection:      "Long",
		PriceDenom:             "USDC",
		AssetDenom:             "ATOM",
		Quantity:               sdk.NewDec(1),
		ExecutionCostOrProceed: sdk.NewDec(95),
		ExpectedCostOrProceed:  sdk.NewDec(100),
		Account:                "abc",
		OrderType:              "Limit",
		OrderId:                1,
		Timestamp:              TestTimestamp,
		Height:                 TestHeight,
	})
	assert.Equal(t, *settlements[3], types.SettlementEntry{
		PositionDirection:      "Short",
		PriceDenom:             "USDC",
		AssetDenom:             "ATOM",
		Quantity:               sdk.NewDec(1),
		ExecutionCostOrProceed: sdk.NewDec(95),
		ExpectedCostOrProceed:  sdk.NewDec(90),
		Account:                "def",
		OrderType:              "Limit",
		OrderId:                4,
		Timestamp:              TestTimestamp,
		Height:                 TestHeight,
	})
	assert.Equal(t, *settlements[4], types.SettlementEntry{
		PositionDirection:      "Long",
		PriceDenom:             "USDC",
		AssetDenom:             "ATOM",
		Quantity:               sdk.NewDec(4),
		ExecutionCostOrProceed: sdk.NewDec(100),
		ExpectedCostOrProceed:  sdk.NewDec(100),
		Account:                "abc",
		OrderType:              "Limit",
		OrderId:                1,
		Timestamp:              TestTimestamp,
		Height:                 TestHeight,
	})
	assert.Equal(t, *settlements[5], types.SettlementEntry{
		PositionDirection:      "Short",
		PriceDenom:             "USDC",
		AssetDenom:             "ATOM",
		Quantity:               sdk.NewDec(4),
		ExecutionCostOrProceed: sdk.NewDec(100),
		ExpectedCostOrProceed:  sdk.NewDec(100),
		Account:                "def",
		OrderType:              "Limit",
		OrderId:                5,
		Timestamp:              TestTimestamp,
		Height:                 TestHeight,
	})
}

func TestMatchMultipleOrderFromMultipleLongBook(t *testing.T) {
	dexkeeper, ctx := keepertest.DexKeeper(t)
	ctx = ctx.WithBlockHeight(int64(TestHeight)).WithBlockTime(time.Unix(int64(TestTimestamp), 0))
	longOrders := []*types.Order{}
	shortOrders := []*types.Order{
		{
			Id:                1,
			Price:             sdk.NewDec(100),
			Quantity:          sdk.NewDec(5),
			Account:           "abc",
			PositionDirection: types.PositionDirection_SHORT,
			ContractAddr:      "test",
			PriceDenom:        "USDC",
			AssetDenom:        "ATOM",
			OrderType:         types.OrderType_LIMIT,
		},
		{
			Id:                2,
			Price:             sdk.NewDec(96),
			Quantity:          sdk.NewDec(1),
			Account:           "jkl",
			PositionDirection: types.PositionDirection_SHORT,
			ContractAddr:      "test",
			PriceDenom:        "USDC",
			AssetDenom:        "ATOM",
			OrderType:         types.OrderType_LIMIT,
		},
		{
			Id:                3,
			Price:             sdk.NewDec(102),
			Quantity:          sdk.NewDec(2),
			Account:           "mno",
			PositionDirection: types.PositionDirection_SHORT,
			ContractAddr:      "test",
			PriceDenom:        "USDC",
			AssetDenom:        "ATOM",
			OrderType:         types.OrderType_LIMIT,
		},
	}
	longBook := []types.OrderBookEntry{
		&types.LongBook{
			Price: sdk.NewDec(100),
			Entry: &types.OrderEntry{
				Price:    sdk.NewDec(100),
				Quantity: sdk.NewDec(6),
				Allocations: []*types.Allocation{{
					OrderId:  4,
					Account:  "abc",
					Quantity: sdk.NewDec(4),
				}, {
					OrderId:  5,
					Account:  "ghi",
					Quantity: sdk.NewDec(2),
				}},
				PriceDenom: "USDC",
				AssetDenom: "ATOM",
			},
		},
		&types.LongBook{
			Price: sdk.NewDec(110),
			Entry: &types.OrderEntry{
				Price:    sdk.NewDec(110),
				Quantity: sdk.NewDec(2),
				Allocations: []*types.Allocation{{
					OrderId:  6,
					Account:  "abc",
					Quantity: sdk.NewDec(2),
				}},
				PriceDenom: "USDC",
				AssetDenom: "ATOM",
			},
		},
	}
	shortBook := []types.OrderBookEntry{}
	for _, e := range longBook {
		dexkeeper.SetLongOrderBookEntry(ctx, "test", e)
	}
	for _, e := range shortBook {
		dexkeeper.SetShortOrderBookEntry(ctx, "test", e)
	}
	exchange.AddOutstandingLimitOrdersToOrderbook(ctx, dexkeeper, longOrders, shortOrders)
	orderbook := keeperutil.PopulateOrderbook(ctx, dexkeeper, types.ContractAddress("test"), types.Pair{PriceDenom: "USDC", AssetDenom: "ATOM"})
	outcome := exchange.MatchLimitOrders(
		ctx, orderbook,
	)
	totalPrice := outcome.TotalNotional
	totalExecuted := outcome.TotalQuantity
	settlements := outcome.Settlements
	minPrice := outcome.MinPrice
	maxPrice := outcome.MaxPrice
	assert.Equal(t, totalPrice, sdk.NewDec(1216))
	assert.Equal(t, totalExecuted, sdk.NewDec(12))
	assert.Equal(t, minPrice, sdk.NewDec(96))
	assert.Equal(t, maxPrice, sdk.NewDec(110))
	longBook = dexkeeper.GetAllLongBookForPair(ctx, "test", "USDC", "ATOM")
	shortBook = dexkeeper.GetAllShortBookForPair(ctx, "test", "USDC", "ATOM")
	assert.Equal(t, len(longBook), 1)
	assert.Equal(t, longBook[0].GetPrice(), sdk.NewDec(100))
	assert.Equal(t, *longBook[0].GetOrderEntry(), types.OrderEntry{
		Price:    sdk.NewDec(100),
		Quantity: sdk.NewDec(2),
		Allocations: []*types.Allocation{{
			OrderId:  5,
			Account:  "ghi",
			Quantity: sdk.NewDec(2),
		}},
		PriceDenom: "USDC",
		AssetDenom: "ATOM",
	})
	assert.Equal(t, len(shortBook), 1)
	assert.Equal(t, shortBook[0].GetPrice(), sdk.NewDec(102))
	assert.Equal(t, *shortBook[0].GetOrderEntry(), types.OrderEntry{
		Price:    sdk.NewDec(102),
		Quantity: sdk.NewDec(2),
		Allocations: []*types.Allocation{{
			OrderId:  3,
			Account:  "mno",
			Quantity: sdk.NewDec(2),
		}},
		PriceDenom: "USDC",
		AssetDenom: "ATOM",
	})
	assert.Equal(t, len(settlements), 6)
	assert.Equal(t, *settlements[0], types.SettlementEntry{
		PositionDirection:      "Long",
		PriceDenom:             "USDC",
		AssetDenom:             "ATOM",
		Quantity:               sdk.NewDec(1),
		ExecutionCostOrProceed: sdk.NewDec(103),
		ExpectedCostOrProceed:  sdk.NewDec(110),
		Account:                "abc",
		OrderType:              "Limit",
		OrderId:                6,
		Timestamp:              TestTimestamp,
		Height:                 TestHeight,
	})
	assert.Equal(t, *settlements[1], types.SettlementEntry{
		PositionDirection:      "Short",
		PriceDenom:             "USDC",
		AssetDenom:             "ATOM",
		Quantity:               sdk.NewDec(1),
		ExecutionCostOrProceed: sdk.NewDec(103),
		ExpectedCostOrProceed:  sdk.NewDec(96),
		Account:                "jkl",
		OrderType:              "Limit",
		OrderId:                2,
		Timestamp:              TestTimestamp,
		Height:                 TestHeight,
	})
	assert.Equal(t, *settlements[2], types.SettlementEntry{
		PositionDirection:      "Long",
		PriceDenom:             "USDC",
		AssetDenom:             "ATOM",
		Quantity:               sdk.NewDec(1),
		ExecutionCostOrProceed: sdk.NewDec(105),
		ExpectedCostOrProceed:  sdk.NewDec(110),
		Account:                "abc",
		OrderType:              "Limit",
		OrderId:                6,
		Timestamp:              TestTimestamp,
		Height:                 TestHeight,
	})
	assert.Equal(t, *settlements[3], types.SettlementEntry{
		PositionDirection:      "Short",
		PriceDenom:             "USDC",
		AssetDenom:             "ATOM",
		Quantity:               sdk.NewDec(1),
		ExecutionCostOrProceed: sdk.NewDec(105),
		ExpectedCostOrProceed:  sdk.NewDec(100),
		Account:                "abc",
		OrderType:              "Limit",
		OrderId:                1,
		Timestamp:              TestTimestamp,
		Height:                 TestHeight,
	})
	assert.Equal(t, *settlements[4], types.SettlementEntry{
		PositionDirection:      "Long",
		PriceDenom:             "USDC",
		AssetDenom:             "ATOM",
		Quantity:               sdk.NewDec(4),
		ExecutionCostOrProceed: sdk.NewDec(100),
		ExpectedCostOrProceed:  sdk.NewDec(100),
		Account:                "abc",
		OrderType:              "Limit",
		OrderId:                4,
		Timestamp:              TestTimestamp,
		Height:                 TestHeight,
	})
	assert.Equal(t, *settlements[5], types.SettlementEntry{
		PositionDirection:      "Short",
		PriceDenom:             "USDC",
		AssetDenom:             "ATOM",
		Quantity:               sdk.NewDec(4),
		ExecutionCostOrProceed: sdk.NewDec(100),
		ExpectedCostOrProceed:  sdk.NewDec(100),
		Account:                "abc",
		OrderType:              "Limit",
		OrderId:                1,
		Timestamp:              TestTimestamp,
		Height:                 TestHeight,
	})
}
