package exchange_test

import (
	"testing"
	"time"

	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/sei-protocol/sei-chain/utils/datastructures"
	"github.com/sei-protocol/sei-chain/x/dex/exchange"
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
	_, ctx := keepertest.DexKeeper(t)
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
	orderbook := types.OrderBook{
		Longs: &types.CachedSortedOrderBookEntries{
			Entries:      longBook,
			DirtyEntries: datastructures.NewTypedSyncMap[string, types.OrderBookEntry](),
		},
		Shorts: &types.CachedSortedOrderBookEntries{
			Entries:      shortBook,
			DirtyEntries: datastructures.NewTypedSyncMap[string, types.OrderBookEntry](),
		},
	}
	outcome := exchange.MatchLimitOrders(
		ctx, longOrders, shortOrders, &orderbook,
	)
	totalPrice := outcome.TotalNotional
	totalExecuted := outcome.TotalQuantity
	settlements := outcome.Settlements
	assert.Equal(t, totalPrice, sdk.NewDec(1000))
	assert.Equal(t, totalExecuted, sdk.NewDec(10))
	assert.Equal(t, orderbook.Longs.DirtyEntries.Len(), 1)
	assert.Equal(t, orderbook.Shorts.DirtyEntries.Len(), 1)
	_, ok := orderbook.Longs.DirtyEntries.Load(sdk.MustNewDecFromStr("100").String())
	assert.True(t, ok)
	_, ok = orderbook.Shorts.DirtyEntries.Load(sdk.MustNewDecFromStr("100").String())
	assert.True(t, ok)
	longBook = orderbook.Longs.Entries
	shortBook = orderbook.Shorts.Entries
	assert.Equal(t, len(longBook), 1)
	assert.Equal(t, longBook[0].GetPrice(), sdk.NewDec(100))
	assert.Equal(t, longBook[0].GetEntry().Price, sdk.NewDec(100))
	assert.Equal(t, longBook[0].GetEntry().Quantity.IsZero(), true)
	assert.Equal(t, len(shortBook), 1)
	assert.Equal(t, shortBook[0].GetPrice(), sdk.NewDec(100))
	assert.Equal(t, shortBook[0].GetEntry().Price, sdk.NewDec(100))
	assert.Equal(t, shortBook[0].GetEntry().Quantity.IsZero(), true)
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
	_, ctx := keepertest.DexKeeper(t)
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
	orderbook := types.OrderBook{
		Longs: &types.CachedSortedOrderBookEntries{
			Entries:      longBook,
			DirtyEntries: datastructures.NewTypedSyncMap[string, types.OrderBookEntry](),
		},
		Shorts: &types.CachedSortedOrderBookEntries{
			Entries:      shortBook,
			DirtyEntries: datastructures.NewTypedSyncMap[string, types.OrderBookEntry](),
		},
	}
	outcome := exchange.MatchLimitOrders(
		ctx, longOrders, shortOrders, &orderbook,
	)
	totalPrice := outcome.TotalNotional
	totalExecuted := outcome.TotalQuantity
	settlements := outcome.Settlements
	assert.Equal(t, totalPrice, sdk.NewDec(0))
	assert.Equal(t, totalExecuted, sdk.NewDec(0))
	assert.Equal(t, orderbook.Longs.DirtyEntries.Len(), 2)
	assert.Equal(t, orderbook.Shorts.DirtyEntries.Len(), 2)
	_, ok := orderbook.Longs.DirtyEntries.Load(sdk.MustNewDecFromStr("95").String())
	assert.True(t, ok)
	_, ok = orderbook.Longs.DirtyEntries.Load(sdk.MustNewDecFromStr("100").String())
	assert.True(t, ok)
	_, ok = orderbook.Shorts.DirtyEntries.Load(sdk.MustNewDecFromStr("105").String())
	assert.True(t, ok)
	_, ok = orderbook.Shorts.DirtyEntries.Load(sdk.MustNewDecFromStr("115").String())
	assert.True(t, ok)
	longBook = orderbook.Longs.Entries
	shortBook = orderbook.Shorts.Entries
	assert.Equal(t, len(longBook), 3)
	assert.Equal(t, longBook[0].GetPrice(), sdk.NewDec(95))
	assert.Equal(t, *longBook[0].GetEntry(), types.OrderEntry{
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
	assert.Equal(t, *longBook[1].GetEntry(), types.OrderEntry{
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
	assert.Equal(t, *longBook[2].GetEntry(), types.OrderEntry{
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
	assert.Equal(t, *shortBook[0].GetEntry(), types.OrderEntry{
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
	assert.Equal(t, *shortBook[1].GetEntry(), types.OrderEntry{
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
	assert.Equal(t, *shortBook[2].GetEntry(), types.OrderEntry{
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
	_, ctx := keepertest.DexKeeper(t)
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
	orderbook := types.OrderBook{
		Longs: &types.CachedSortedOrderBookEntries{
			Entries:      longBook,
			DirtyEntries: datastructures.NewTypedSyncMap[string, types.OrderBookEntry](),
		},
		Shorts: &types.CachedSortedOrderBookEntries{
			Entries:      shortBook,
			DirtyEntries: datastructures.NewTypedSyncMap[string, types.OrderBookEntry](),
		},
	}
	outcome := exchange.MatchLimitOrders(
		ctx, longOrders, shortOrders, &orderbook,
	)
	totalPrice := outcome.TotalNotional
	totalExecuted := outcome.TotalQuantity
	settlements := outcome.Settlements
	assert.Equal(t, totalPrice, sdk.NewDec(1000))
	assert.Equal(t, totalExecuted, sdk.NewDec(10))
	assert.Equal(t, orderbook.Longs.DirtyEntries.Len(), 1)
	assert.Equal(t, orderbook.Shorts.DirtyEntries.Len(), 1)
	_, ok := orderbook.Longs.DirtyEntries.Load(sdk.MustNewDecFromStr("100").String())
	assert.True(t, ok)
	_, ok = orderbook.Shorts.DirtyEntries.Load(sdk.MustNewDecFromStr("100").String())
	assert.True(t, ok)
	longBook = orderbook.Longs.Entries
	shortBook = orderbook.Shorts.Entries
	assert.Equal(t, len(longBook), 1)
	assert.Equal(t, longBook[0].GetPrice(), sdk.NewDec(100))
	assert.Equal(t, longBook[0].GetEntry().Price, sdk.NewDec(100))
	assert.Equal(t, longBook[0].GetEntry().Quantity.IsZero(), true)
	assert.Equal(t, len(shortBook), 1)
	assert.Equal(t, shortBook[0].GetPrice(), sdk.NewDec(100))
	assert.Equal(t, shortBook[0].GetEntry().Price, sdk.NewDec(100))
	assert.Equal(t, shortBook[0].GetEntry().Quantity.IsZero(), true)
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
	_, ctx := keepertest.DexKeeper(t)
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
	orderbook := types.OrderBook{
		Longs: &types.CachedSortedOrderBookEntries{
			Entries:      longBook,
			DirtyEntries: datastructures.NewTypedSyncMap[string, types.OrderBookEntry](),
		},
		Shorts: &types.CachedSortedOrderBookEntries{
			Entries:      shortBook,
			DirtyEntries: datastructures.NewTypedSyncMap[string, types.OrderBookEntry](),
		},
	}
	outcome := exchange.MatchLimitOrders(
		ctx, longOrders, shortOrders, &orderbook,
	)
	totalPrice := outcome.TotalNotional
	totalExecuted := outcome.TotalQuantity
	settlements := outcome.Settlements
	assert.Equal(t, totalPrice, sdk.NewDec(1000))
	assert.Equal(t, totalExecuted, sdk.NewDec(10))
	assert.Equal(t, orderbook.Longs.DirtyEntries.Len(), 1)
	assert.Equal(t, orderbook.Shorts.DirtyEntries.Len(), 1)
	_, ok := orderbook.Longs.DirtyEntries.Load(sdk.MustNewDecFromStr("100").String())
	assert.True(t, ok)
	_, ok = orderbook.Shorts.DirtyEntries.Load(sdk.MustNewDecFromStr("100").String())
	assert.True(t, ok)
	longBook = orderbook.Longs.Entries
	shortBook = orderbook.Shorts.Entries
	assert.Equal(t, len(longBook), 1)
	assert.Equal(t, longBook[0].GetPrice(), sdk.NewDec(100))
	assert.Equal(t, longBook[0].GetEntry().Price, sdk.NewDec(100))
	assert.Equal(t, longBook[0].GetEntry().Quantity.IsZero(), true)
	assert.Equal(t, len(shortBook), 1)
	assert.Equal(t, shortBook[0].GetPrice(), sdk.NewDec(100))
	assert.Equal(t, shortBook[0].GetEntry().Price, sdk.NewDec(100))
	assert.Equal(t, shortBook[0].GetEntry().Quantity.IsZero(), true)
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
	_, ctx := keepertest.DexKeeper(t)
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
	orderbook := types.OrderBook{
		Longs: &types.CachedSortedOrderBookEntries{
			Entries:      longBook,
			DirtyEntries: datastructures.NewTypedSyncMap[string, types.OrderBookEntry](),
		},
		Shorts: &types.CachedSortedOrderBookEntries{
			Entries:      shortBook,
			DirtyEntries: datastructures.NewTypedSyncMap[string, types.OrderBookEntry](),
		},
	}
	outcome := exchange.MatchLimitOrders(
		ctx, longOrders, shortOrders, &orderbook,
	)
	totalPrice := outcome.TotalNotional
	totalExecuted := outcome.TotalQuantity
	settlements := outcome.Settlements
	assert.Equal(t, totalPrice, sdk.NewDec(980))
	assert.Equal(t, totalExecuted, sdk.NewDec(10))
	assert.Equal(t, orderbook.Longs.DirtyEntries.Len(), 1)
	assert.Equal(t, orderbook.Shorts.DirtyEntries.Len(), 2)
	_, ok := orderbook.Longs.DirtyEntries.Load(sdk.MustNewDecFromStr("100").String())
	assert.True(t, ok)
	_, ok = orderbook.Shorts.DirtyEntries.Load(sdk.MustNewDecFromStr("100").String())
	assert.True(t, ok)
	_, ok = orderbook.Shorts.DirtyEntries.Load(sdk.MustNewDecFromStr("90").String())
	assert.True(t, ok)
	longBook = orderbook.Longs.Entries
	shortBook = orderbook.Shorts.Entries
	assert.Equal(t, len(longBook), 1)
	assert.Equal(t, longBook[0].GetPrice(), sdk.NewDec(100))
	assert.Equal(t, longBook[0].GetEntry().Price, sdk.NewDec(100))
	assert.Equal(t, longBook[0].GetEntry().Quantity.IsZero(), true)
	assert.Equal(t, len(shortBook), 2)
	assert.Equal(t, shortBook[0].GetPrice(), sdk.NewDec(90))
	assert.Equal(t, shortBook[0].GetEntry().Price, sdk.NewDec(90))
	assert.Equal(t, shortBook[0].GetEntry().Quantity.IsZero(), true)
	assert.Equal(t, shortBook[1].GetPrice(), sdk.NewDec(100))
	assert.Equal(t, *shortBook[1].GetEntry(), types.OrderEntry{
		Price:    sdk.NewDec(100),
		Quantity: sdk.NewDec(3),
		Allocations: []*types.Allocation{{
			OrderId:  3,
			Account:  "def",
			Quantity: sdk.NewDec(2),
		}, {
			OrderId:  4,
			Account:  "ghi",
			Quantity: sdk.NewDec(1),
		}},
		PriceDenom: "USDC",
		AssetDenom: "ATOM",
	})
	assert.Equal(t, len(settlements), 6)
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
		Quantity:               sdk.NewDec(2),
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
		Quantity:               sdk.NewDec(2),
		ExecutionCostOrProceed: sdk.NewDec(100),
		ExpectedCostOrProceed:  sdk.NewDec(100),
		Account:                "def",
		OrderType:              "Limit",
		OrderId:                3,
		Timestamp:              TestTimestamp,
		Height:                 TestHeight,
	})
	assert.Equal(t, *settlements[4], types.SettlementEntry{
		PositionDirection:      "Long",
		PriceDenom:             "USDC",
		AssetDenom:             "ATOM",
		Quantity:               sdk.NewDec(1),
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
		Quantity:               sdk.NewDec(1),
		ExecutionCostOrProceed: sdk.NewDec(100),
		ExpectedCostOrProceed:  sdk.NewDec(100),
		Account:                "ghi",
		OrderType:              "Limit",
		OrderId:                4,
		Timestamp:              TestTimestamp,
		Height:                 TestHeight,
	})
}

func TestMatchSingleOrderFromMultipleLongBook(t *testing.T) {
	_, ctx := keepertest.DexKeeper(t)
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
	orderbook := types.OrderBook{
		Longs: &types.CachedSortedOrderBookEntries{
			Entries:      longBook,
			DirtyEntries: datastructures.NewTypedSyncMap[string, types.OrderBookEntry](),
		},
		Shorts: &types.CachedSortedOrderBookEntries{
			Entries:      shortBook,
			DirtyEntries: datastructures.NewTypedSyncMap[string, types.OrderBookEntry](),
		},
	}
	outcome := exchange.MatchLimitOrders(
		ctx, longOrders, shortOrders, &orderbook,
	)
	totalPrice := outcome.TotalNotional
	totalExecuted := outcome.TotalQuantity
	settlements := outcome.Settlements
	assert.Equal(t, totalPrice, sdk.NewDec(1020))
	assert.Equal(t, totalExecuted, sdk.NewDec(10))
	assert.Equal(t, orderbook.Longs.DirtyEntries.Len(), 2)
	assert.Equal(t, orderbook.Shorts.DirtyEntries.Len(), 1)
	_, ok := orderbook.Longs.DirtyEntries.Load(sdk.MustNewDecFromStr("100").String())
	assert.True(t, ok)
	_, ok = orderbook.Longs.DirtyEntries.Load(sdk.MustNewDecFromStr("110").String())
	assert.True(t, ok)
	_, ok = orderbook.Shorts.DirtyEntries.Load(sdk.MustNewDecFromStr("100").String())
	assert.True(t, ok)
	longBook = orderbook.Longs.Entries
	shortBook = orderbook.Shorts.Entries
	assert.Equal(t, len(longBook), 2)
	assert.Equal(t, longBook[0].GetPrice(), sdk.NewDec(100))
	assert.Equal(t, *longBook[0].GetEntry(), types.OrderEntry{
		Price:    sdk.NewDec(100),
		Quantity: sdk.NewDec(3),
		Allocations: []*types.Allocation{{
			OrderId:  2,
			Account:  "abc",
			Quantity: sdk.NewDec(2),
		}, {
			OrderId:  3,
			Account:  "ghi",
			Quantity: sdk.NewDec(1),
		}},
		PriceDenom: "USDC",
		AssetDenom: "ATOM",
	})
	assert.Equal(t, longBook[1].GetPrice(), sdk.NewDec(110))
	assert.Equal(t, longBook[1].GetEntry().Price, sdk.NewDec(110))
	assert.Equal(t, longBook[1].GetEntry().Quantity.IsZero(), true)
	assert.Equal(t, len(shortBook), 1)
	assert.Equal(t, shortBook[0].GetPrice(), sdk.NewDec(100))
	assert.Equal(t, shortBook[0].GetEntry().Price, sdk.NewDec(100))
	assert.Equal(t, shortBook[0].GetEntry().Quantity.IsZero(), true)
	assert.Equal(t, len(settlements), 6)
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
		Quantity:               sdk.NewDec(2),
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
		Quantity:               sdk.NewDec(2),
		ExecutionCostOrProceed: sdk.NewDec(100),
		ExpectedCostOrProceed:  sdk.NewDec(100),
		Account:                "def",
		OrderType:              "Limit",
		OrderId:                1,
		Timestamp:              TestTimestamp,
		Height:                 TestHeight,
	})
	assert.Equal(t, *settlements[4], types.SettlementEntry{
		PositionDirection:      "Long",
		PriceDenom:             "USDC",
		AssetDenom:             "ATOM",
		Quantity:               sdk.NewDec(1),
		ExecutionCostOrProceed: sdk.NewDec(100),
		ExpectedCostOrProceed:  sdk.NewDec(100),
		Account:                "ghi",
		OrderType:              "Limit",
		OrderId:                3,
		Timestamp:              TestTimestamp,
		Height:                 TestHeight,
	})
	assert.Equal(t, *settlements[5], types.SettlementEntry{
		PositionDirection:      "Short",
		PriceDenom:             "USDC",
		AssetDenom:             "ATOM",
		Quantity:               sdk.NewDec(1),
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
	_, ctx := keepertest.DexKeeper(t)
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
	orderbook := types.OrderBook{
		Longs: &types.CachedSortedOrderBookEntries{
			Entries:      longBook,
			DirtyEntries: datastructures.NewTypedSyncMap[string, types.OrderBookEntry](),
		},
		Shorts: &types.CachedSortedOrderBookEntries{
			Entries:      shortBook,
			DirtyEntries: datastructures.NewTypedSyncMap[string, types.OrderBookEntry](),
		},
	}
	outcome := exchange.MatchLimitOrders(
		ctx, longOrders, shortOrders, &orderbook,
	)
	totalPrice := outcome.TotalNotional
	totalExecuted := outcome.TotalQuantity
	settlements := outcome.Settlements
	assert.Equal(t, totalPrice, sdk.NewDec(1184))
	assert.Equal(t, totalExecuted, sdk.NewDec(12))
	assert.Equal(t, orderbook.Longs.DirtyEntries.Len(), 3)
	assert.Equal(t, orderbook.Shorts.DirtyEntries.Len(), 2)
	_, ok := orderbook.Longs.DirtyEntries.Load(sdk.MustNewDecFromStr("98").String())
	assert.True(t, ok)
	_, ok = orderbook.Longs.DirtyEntries.Load(sdk.MustNewDecFromStr("100").String())
	assert.True(t, ok)
	_, ok = orderbook.Longs.DirtyEntries.Load(sdk.MustNewDecFromStr("104").String())
	assert.True(t, ok)
	_, ok = orderbook.Shorts.DirtyEntries.Load(sdk.MustNewDecFromStr("100").String())
	assert.True(t, ok)
	_, ok = orderbook.Shorts.DirtyEntries.Load(sdk.MustNewDecFromStr("90").String())
	assert.True(t, ok)
	longBook = orderbook.Longs.Entries
	shortBook = orderbook.Shorts.Entries
	assert.Equal(t, len(longBook), 3)
	assert.Equal(t, longBook[0].GetPrice(), sdk.NewDec(98))
	assert.Equal(t, *longBook[0].GetEntry(), types.OrderEntry{
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
	assert.Equal(t, longBook[1].GetPrice(), sdk.NewDec(100))
	assert.Equal(t, longBook[1].GetEntry().Price, sdk.NewDec(100))
	assert.Equal(t, longBook[1].GetEntry().Quantity.IsZero(), true)
	assert.Equal(t, longBook[2].GetPrice(), sdk.NewDec(104))
	assert.Equal(t, longBook[2].GetEntry().Price, sdk.NewDec(104))
	assert.Equal(t, longBook[2].GetEntry().Quantity.IsZero(), true)
	assert.Equal(t, len(shortBook), 2)
	assert.Equal(t, shortBook[0].GetPrice(), sdk.NewDec(90))
	assert.Equal(t, shortBook[0].GetEntry().Price, sdk.NewDec(90))
	assert.Equal(t, shortBook[0].GetEntry().Quantity.IsZero(), true)
	assert.Equal(t, shortBook[1].GetPrice(), sdk.NewDec(100))
	assert.Equal(t, *shortBook[1].GetEntry(), types.OrderEntry{
		Price:    sdk.NewDec(100),
		Quantity: sdk.NewDec(2),
		Allocations: []*types.Allocation{{
			OrderId:  5,
			Account:  "def",
			Quantity: sdk.NewDec(4).Quo(sdk.NewDec(3)),
		}, {
			OrderId:  6,
			Account:  "ghi",
			Quantity: sdk.NewDec(2).Quo(sdk.NewDec(3)),
		}},
		PriceDenom: "USDC",
		AssetDenom: "ATOM",
	})
	assert.Equal(t, len(settlements), 8)
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
		Quantity:               sdk.NewDec(8).Quo(sdk.NewDec(3)),
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
		Quantity:               sdk.NewDec(8).Quo(sdk.NewDec(3)),
		ExecutionCostOrProceed: sdk.NewDec(100),
		ExpectedCostOrProceed:  sdk.NewDec(100),
		Account:                "def",
		OrderType:              "Limit",
		OrderId:                5,
		Timestamp:              TestTimestamp,
		Height:                 TestHeight,
	})
	assert.Equal(t, *settlements[6], types.SettlementEntry{
		PositionDirection:      "Long",
		PriceDenom:             "USDC",
		AssetDenom:             "ATOM",
		Quantity:               sdk.NewDec(4).Quo(sdk.NewDec(3)),
		ExecutionCostOrProceed: sdk.NewDec(100),
		ExpectedCostOrProceed:  sdk.NewDec(100),
		Account:                "abc",
		OrderType:              "Limit",
		OrderId:                1,
		Timestamp:              TestTimestamp,
		Height:                 TestHeight,
	})
	assert.Equal(t, *settlements[7], types.SettlementEntry{
		PositionDirection:      "Short",
		PriceDenom:             "USDC",
		AssetDenom:             "ATOM",
		Quantity:               sdk.NewDec(4).Quo(sdk.NewDec(3)),
		ExecutionCostOrProceed: sdk.NewDec(100),
		ExpectedCostOrProceed:  sdk.NewDec(100),
		Account:                "ghi",
		OrderType:              "Limit",
		OrderId:                6,
		Timestamp:              TestTimestamp,
		Height:                 TestHeight,
	})
}

func TestMatchMultipleOrderFromMultipleLongBook(t *testing.T) {
	_, ctx := keepertest.DexKeeper(t)
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
	orderbook := types.OrderBook{
		Longs: &types.CachedSortedOrderBookEntries{
			Entries:      longBook,
			DirtyEntries: datastructures.NewTypedSyncMap[string, types.OrderBookEntry](),
		},
		Shorts: &types.CachedSortedOrderBookEntries{
			Entries:      shortBook,
			DirtyEntries: datastructures.NewTypedSyncMap[string, types.OrderBookEntry](),
		},
	}
	outcome := exchange.MatchLimitOrders(
		ctx, longOrders, shortOrders, &orderbook,
	)
	totalPrice := outcome.TotalNotional
	totalExecuted := outcome.TotalQuantity
	settlements := outcome.Settlements
	assert.Equal(t, totalPrice, sdk.NewDec(1216))
	assert.Equal(t, totalExecuted, sdk.NewDec(12))
	assert.Equal(t, orderbook.Longs.DirtyEntries.Len(), 2)
	assert.Equal(t, orderbook.Shorts.DirtyEntries.Len(), 3)
	_, ok := orderbook.Longs.DirtyEntries.Load(sdk.MustNewDecFromStr("100").String())
	assert.True(t, ok)
	_, ok = orderbook.Longs.DirtyEntries.Load(sdk.MustNewDecFromStr("110").String())
	assert.True(t, ok)
	_, ok = orderbook.Shorts.DirtyEntries.Load(sdk.MustNewDecFromStr("96").String())
	assert.True(t, ok)
	_, ok = orderbook.Shorts.DirtyEntries.Load(sdk.MustNewDecFromStr("100").String())
	assert.True(t, ok)
	_, ok = orderbook.Shorts.DirtyEntries.Load(sdk.MustNewDecFromStr("102").String())
	assert.True(t, ok)
	longBook = orderbook.Longs.Entries
	shortBook = orderbook.Shorts.Entries
	assert.Equal(t, len(longBook), 2)
	assert.Equal(t, longBook[0].GetPrice(), sdk.NewDec(100))
	assert.Equal(t, *longBook[0].GetEntry(), types.OrderEntry{
		Price:    sdk.NewDec(100),
		Quantity: sdk.NewDec(2),
		Allocations: []*types.Allocation{{
			OrderId:  4,
			Account:  "abc",
			Quantity: sdk.NewDec(4).Quo(sdk.NewDec(3)),
		}, {
			OrderId:  5,
			Account:  "ghi",
			Quantity: sdk.NewDec(2).Quo(sdk.NewDec(3)),
		}},
		PriceDenom: "USDC",
		AssetDenom: "ATOM",
	})
	assert.Equal(t, longBook[1].GetPrice(), sdk.NewDec(110))
	assert.Equal(t, longBook[1].GetEntry().Price, sdk.NewDec(110))
	assert.Equal(t, longBook[1].GetEntry().Quantity.IsZero(), true)
	assert.Equal(t, len(shortBook), 3)
	assert.Equal(t, shortBook[0].GetPrice(), sdk.NewDec(96))
	assert.Equal(t, shortBook[0].GetEntry().Price, sdk.NewDec(96))
	assert.Equal(t, shortBook[0].GetEntry().Quantity.IsZero(), true)
	assert.Equal(t, shortBook[1].GetPrice(), sdk.NewDec(100))
	assert.Equal(t, shortBook[1].GetEntry().Price, sdk.NewDec(100))
	assert.Equal(t, shortBook[1].GetEntry().Quantity.IsZero(), true)
	assert.Equal(t, shortBook[2].GetPrice(), sdk.NewDec(102))
	assert.Equal(t, *shortBook[2].GetEntry(), types.OrderEntry{
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
	assert.Equal(t, len(settlements), 8)
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
		Quantity:               sdk.NewDec(8).Quo(sdk.NewDec(3)),
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
		Quantity:               sdk.NewDec(8).Quo(sdk.NewDec(3)),
		ExecutionCostOrProceed: sdk.NewDec(100),
		ExpectedCostOrProceed:  sdk.NewDec(100),
		Account:                "abc",
		OrderType:              "Limit",
		OrderId:                1,
		Timestamp:              TestTimestamp,
		Height:                 TestHeight,
	})
	assert.Equal(t, *settlements[6], types.SettlementEntry{
		PositionDirection:      "Long",
		PriceDenom:             "USDC",
		AssetDenom:             "ATOM",
		Quantity:               sdk.NewDec(4).Quo(sdk.NewDec(3)),
		ExecutionCostOrProceed: sdk.NewDec(100),
		ExpectedCostOrProceed:  sdk.NewDec(100),
		Account:                "ghi",
		OrderType:              "Limit",
		OrderId:                5,
		Timestamp:              TestTimestamp,
		Height:                 TestHeight,
	})
	assert.Equal(t, *settlements[7], types.SettlementEntry{
		PositionDirection:      "Short",
		PriceDenom:             "USDC",
		AssetDenom:             "ATOM",
		Quantity:               sdk.NewDec(4).Quo(sdk.NewDec(3)),
		ExecutionCostOrProceed: sdk.NewDec(100),
		ExpectedCostOrProceed:  sdk.NewDec(100),
		Account:                "abc",
		OrderType:              "Limit",
		OrderId:                1,
		Timestamp:              TestTimestamp,
		Height:                 TestHeight,
	})
}
