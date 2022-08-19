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

const (
	TestTimestamp uint64 = 10000
	TestHeight    uint64 = 1
)

func TestMatchFoKMarketOrderFromShortBookNotEnoughLiquidity(t *testing.T) {
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
			OrderType:         types.OrderType_FOKMARKET,
		},
	}
	shortBook := []types.OrderBookEntry{
		&types.ShortBook{
			Price: sdk.NewDec(100),
			Entry: &types.OrderEntry{
				Price:    sdk.NewDec(100),
				Quantity: sdk.NewDec(4),
				Allocations: []*types.Allocation{{
					OrderId:  5,
					Account:  "def",
					Quantity: sdk.NewDec(5),
				}},
				PriceDenom: "USDC",
				AssetDenom: "ATOM",
			},
		},
	}
	entries := &types.CachedSortedOrderBookEntries{
		Entries:      shortBook,
		DirtyEntries: datastructures.NewTypedSyncMap[string, types.OrderBookEntry](),
	}
	outcome := exchange.MatchMarketOrders(
		ctx, longOrders, entries, types.PositionDirection_LONG,
	)
	totalPrice := outcome.TotalNotional
	totalExecuted := outcome.TotalQuantity
	settlements := outcome.Settlements
	assert.Equal(t, totalPrice, sdk.ZeroDec())
	assert.Equal(t, totalExecuted, sdk.ZeroDec())
	assert.Equal(t, entries.DirtyEntries.Len(), 0)
	assert.Equal(t, len(shortBook), 1)
	assert.Equal(t, shortBook[0].GetPrice(), sdk.NewDec(100))
	assert.Equal(t, shortBook[0].GetEntry().Price, sdk.NewDec(100))
	assert.Equal(t, shortBook[0].GetEntry().Quantity, sdk.NewDec(4))
	assert.Equal(t, len(settlements), 0)
}

func TestMatchFoKMarketOrderFromShortBookHappyPath(t *testing.T) {
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
			OrderType:         types.OrderType_FOKMARKET,
		},
	}
	shortBook := []types.OrderBookEntry{
		&types.ShortBook{
			Price: sdk.NewDec(100),
			Entry: &types.OrderEntry{
				Price:    sdk.NewDec(100),
				Quantity: sdk.NewDec(5),
				Allocations: []*types.Allocation{{
					OrderId:  5,
					Account:  "def",
					Quantity: sdk.NewDec(5),
				}},
				PriceDenom: "USDC",
				AssetDenom: "ATOM",
			},
		},
	}
	entries := &types.CachedSortedOrderBookEntries{
		Entries:      shortBook,
		DirtyEntries: datastructures.NewTypedSyncMap[string, types.OrderBookEntry](),
	}
	outcome := exchange.MatchMarketOrders(
		ctx, longOrders, entries, types.PositionDirection_LONG,
	)
	totalPrice := outcome.TotalNotional
	totalExecuted := outcome.TotalQuantity
	settlements := outcome.Settlements
	assert.Equal(t, totalPrice, sdk.NewDec(500))
	assert.Equal(t, totalExecuted, sdk.NewDec(5))
	assert.Equal(t, entries.DirtyEntries.Len(), 1)
	_, ok := entries.DirtyEntries.Load(sdk.MustNewDecFromStr("100").String())
	assert.True(t, ok)
	assert.Equal(t, len(shortBook), 1)
	assert.Equal(t, shortBook[0].GetPrice(), sdk.NewDec(100))
	assert.Equal(t, shortBook[0].GetEntry().Price, sdk.NewDec(100))
	assert.Equal(t, shortBook[0].GetEntry().Quantity.IsZero(), true)
	assert.Equal(t, len(settlements), 2)
	assert.Equal(t, *settlements[0], types.SettlementEntry{
		PositionDirection:      "Short",
		PriceDenom:             "USDC",
		AssetDenom:             "ATOM",
		Quantity:               sdk.NewDec(5),
		ExecutionCostOrProceed: sdk.NewDec(100),
		ExpectedCostOrProceed:  sdk.NewDec(100),
		Account:                "def",
		OrderType:              "Limit",
		OrderId:                5,
		Timestamp:              TestTimestamp,
		Height:                 TestHeight,
	})
	assert.Equal(t, *settlements[1], types.SettlementEntry{
		PositionDirection:      "Long",
		PriceDenom:             "USDC",
		AssetDenom:             "ATOM",
		Quantity:               sdk.NewDec(5),
		ExecutionCostOrProceed: sdk.NewDec(100),
		ExpectedCostOrProceed:  sdk.NewDec(100),
		Account:                "abc",
		OrderType:              "Fokmarket",
		OrderId:                1,
		Timestamp:              TestTimestamp,
		Height:                 TestHeight,
	})
}

func TestMatchSingleMarketOrderFromShortBook(t *testing.T) {
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
			OrderType:         types.OrderType_MARKET,
		},
	}
	shortBook := []types.OrderBookEntry{
		&types.ShortBook{
			Price: sdk.NewDec(100),
			Entry: &types.OrderEntry{
				Price:    sdk.NewDec(100),
				Quantity: sdk.NewDec(5),
				Allocations: []*types.Allocation{{
					OrderId:  5,
					Account:  "def",
					Quantity: sdk.NewDec(5),
				}},
				PriceDenom: "USDC",
				AssetDenom: "ATOM",
			},
		},
	}
	entries := &types.CachedSortedOrderBookEntries{
		Entries:      shortBook,
		DirtyEntries: datastructures.NewTypedSyncMap[string, types.OrderBookEntry](),
	}
	outcome := exchange.MatchMarketOrders(
		ctx, longOrders, entries, types.PositionDirection_LONG,
	)
	totalPrice := outcome.TotalNotional
	totalExecuted := outcome.TotalQuantity
	settlements := outcome.Settlements
	assert.Equal(t, totalPrice, sdk.NewDec(500))
	assert.Equal(t, totalExecuted, sdk.NewDec(5))
	assert.Equal(t, entries.DirtyEntries.Len(), 1)
	_, ok := entries.DirtyEntries.Load(sdk.MustNewDecFromStr("100").String())
	assert.True(t, ok)
	assert.Equal(t, len(shortBook), 1)
	assert.Equal(t, shortBook[0].GetPrice(), sdk.NewDec(100))
	assert.Equal(t, shortBook[0].GetEntry().Price, sdk.NewDec(100))
	assert.Equal(t, shortBook[0].GetEntry().Quantity.IsZero(), true)
	assert.Equal(t, len(settlements), 2)
	assert.Equal(t, *settlements[0], types.SettlementEntry{
		PositionDirection:      "Short",
		PriceDenom:             "USDC",
		AssetDenom:             "ATOM",
		Quantity:               sdk.NewDec(5),
		ExecutionCostOrProceed: sdk.NewDec(100),
		ExpectedCostOrProceed:  sdk.NewDec(100),
		Account:                "def",
		OrderType:              "Limit",
		OrderId:                5,
		Timestamp:              TestTimestamp,
		Height:                 TestHeight,
	})
	assert.Equal(t, *settlements[1], types.SettlementEntry{
		PositionDirection:      "Long",
		PriceDenom:             "USDC",
		AssetDenom:             "ATOM",
		Quantity:               sdk.NewDec(5),
		ExecutionCostOrProceed: sdk.NewDec(100),
		ExpectedCostOrProceed:  sdk.NewDec(100),
		Account:                "abc",
		OrderType:              "Market",
		OrderId:                1,
		Timestamp:              TestTimestamp,
		Height:                 TestHeight,
	})
}

func TestMatchSingleMarketOrderFromLongBook(t *testing.T) {
	_, ctx := keepertest.DexKeeper(t)
	ctx = ctx.WithBlockHeight(int64(TestHeight)).WithBlockTime(time.Unix(int64(TestTimestamp), 0))
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
			OrderType:         types.OrderType_MARKET,
		},
	}
	longBook := []types.OrderBookEntry{
		&types.LongBook{
			Price: sdk.NewDec(100),
			Entry: &types.OrderEntry{
				Price:    sdk.NewDec(100),
				Quantity: sdk.NewDec(5),
				Allocations: []*types.Allocation{{
					OrderId:  5,
					Account:  "def",
					Quantity: sdk.NewDec(5),
				}},
				PriceDenom: "USDC",
				AssetDenom: "ATOM",
			},
		},
	}
	entries := &types.CachedSortedOrderBookEntries{
		Entries:      longBook,
		DirtyEntries: datastructures.NewTypedSyncMap[string, types.OrderBookEntry](),
	}
	outcome := exchange.MatchMarketOrders(
		ctx, shortOrders, entries, types.PositionDirection_SHORT,
	)
	totalPrice := outcome.TotalNotional
	totalExecuted := outcome.TotalQuantity
	settlements := outcome.Settlements
	assert.Equal(t, totalPrice, sdk.NewDec(500))
	assert.Equal(t, totalExecuted, sdk.NewDec(5))
	assert.Equal(t, entries.DirtyEntries.Len(), 1)
	_, ok := entries.DirtyEntries.Load(sdk.MustNewDecFromStr("100").String())
	assert.True(t, ok)
	assert.Equal(t, len(longBook), 1)
	assert.Equal(t, longBook[0].GetPrice(), sdk.NewDec(100))
	assert.Equal(t, longBook[0].GetEntry().Price, sdk.NewDec(100))
	assert.Equal(t, longBook[0].GetEntry().Quantity.IsZero(), true)
	assert.Equal(t, len(settlements), 2)
	assert.Equal(t, *settlements[0], types.SettlementEntry{
		PositionDirection:      "Long",
		PriceDenom:             "USDC",
		AssetDenom:             "ATOM",
		Quantity:               sdk.NewDec(5),
		ExecutionCostOrProceed: sdk.NewDec(100),
		ExpectedCostOrProceed:  sdk.NewDec(100),
		Account:                "def",
		OrderType:              "Limit",
		OrderId:                5,
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
		Account:                "abc",
		OrderType:              "Market",
		OrderId:                1,
		Timestamp:              TestTimestamp,
		Height:                 TestHeight,
	})
}

func TestMatchSingleMarketOrderFromMultipleShortBook(t *testing.T) {
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
			OrderType:         types.OrderType_MARKET,
		},
	}
	shortBook := []types.OrderBookEntry{
		&types.ShortBook{
			Price: sdk.NewDec(90),
			Entry: &types.OrderEntry{
				Price:    sdk.NewDec(90),
				Quantity: sdk.NewDec(2),
				Allocations: []*types.Allocation{{
					OrderId:  5,
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
					OrderId:  6,
					Account:  "def",
					Quantity: sdk.NewDec(4),
				}, {
					OrderId:  7,
					Account:  "ghi",
					Quantity: sdk.NewDec(2),
				}},
				PriceDenom: "USDC",
				AssetDenom: "ATOM",
			},
		},
	}
	entries := &types.CachedSortedOrderBookEntries{
		Entries:      shortBook,
		DirtyEntries: datastructures.NewTypedSyncMap[string, types.OrderBookEntry](),
	}
	outcome := exchange.MatchMarketOrders(
		ctx, longOrders, entries, types.PositionDirection_LONG,
	)
	totalPrice := outcome.TotalNotional
	totalExecuted := outcome.TotalQuantity
	settlements := outcome.Settlements
	assert.Equal(t, totalPrice, sdk.NewDec(480))
	assert.Equal(t, totalExecuted, sdk.NewDec(5))
	assert.Equal(t, entries.DirtyEntries.Len(), 2)
	_, ok := entries.DirtyEntries.Load(sdk.MustNewDecFromStr("100").String())
	assert.True(t, ok)
	_, ok = entries.DirtyEntries.Load(sdk.MustNewDecFromStr("90").String())
	assert.True(t, ok)
	assert.Equal(t, len(shortBook), 2)
	assert.Equal(t, shortBook[0].GetPrice(), sdk.NewDec(90))
	assert.Equal(t, shortBook[0].GetEntry().Price, sdk.NewDec(90))
	assert.Equal(t, shortBook[0].GetEntry().Quantity.IsZero(), true)
	assert.Equal(t, shortBook[1].GetPrice(), sdk.NewDec(100))
	assert.Equal(t, *shortBook[1].GetEntry(), types.OrderEntry{
		Price:    sdk.NewDec(100),
		Quantity: sdk.NewDec(3),
		Allocations: []*types.Allocation{{
			OrderId:  6,
			Account:  "def",
			Quantity: sdk.NewDec(1),
		}, {
			OrderId:  7,
			Account:  "ghi",
			Quantity: sdk.NewDec(2),
		}},
		PriceDenom: "USDC",
		AssetDenom: "ATOM",
	})
	assert.Equal(t, len(settlements), 4)
	assert.Equal(t, *settlements[0], types.SettlementEntry{
		PositionDirection:      "Short",
		PriceDenom:             "USDC",
		AssetDenom:             "ATOM",
		Quantity:               sdk.NewDec(2),
		ExecutionCostOrProceed: sdk.NewDec(90),
		ExpectedCostOrProceed:  sdk.NewDec(90),
		Account:                "def",
		OrderType:              "Limit",
		OrderId:                5,
		Timestamp:              TestTimestamp,
		Height:                 TestHeight,
	})
	assert.Equal(t, *settlements[1], types.SettlementEntry{
		PositionDirection:      "Short",
		PriceDenom:             "USDC",
		AssetDenom:             "ATOM",
		Quantity:               sdk.NewDec(3),
		ExecutionCostOrProceed: sdk.NewDec(100),
		ExpectedCostOrProceed:  sdk.NewDec(100),
		Account:                "def",
		OrderType:              "Limit",
		OrderId:                6,
		Timestamp:              TestTimestamp,
		Height:                 TestHeight,
	})
	assert.Equal(t, *settlements[2], types.SettlementEntry{
		PositionDirection:      "Long",
		PriceDenom:             "USDC",
		AssetDenom:             "ATOM",
		Quantity:               sdk.NewDec(2),
		ExecutionCostOrProceed: sdk.NewDec(96),
		ExpectedCostOrProceed:  sdk.NewDec(100),
		Account:                "abc",
		OrderType:              "Market",
		OrderId:                1,
		Timestamp:              TestTimestamp,
		Height:                 TestHeight,
	})
	assert.Equal(t, *settlements[3], types.SettlementEntry{
		PositionDirection:      "Long",
		PriceDenom:             "USDC",
		AssetDenom:             "ATOM",
		Quantity:               sdk.NewDec(3),
		ExecutionCostOrProceed: sdk.NewDec(96),
		ExpectedCostOrProceed:  sdk.NewDec(100),
		Account:                "abc",
		OrderType:              "Market",
		OrderId:                1,
		Timestamp:              TestTimestamp,
		Height:                 TestHeight,
	})
}

func TestMatchSingleMarketOrderFromMultipleLongBook(t *testing.T) {
	_, ctx := keepertest.DexKeeper(t)
	ctx = ctx.WithBlockHeight(int64(TestHeight)).WithBlockTime(time.Unix(int64(TestTimestamp), 0))
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
			OrderType:         types.OrderType_MARKET,
		},
	}
	longBook := []types.OrderBookEntry{
		&types.LongBook{
			Price: sdk.NewDec(100),
			Entry: &types.OrderEntry{
				Price:    sdk.NewDec(100),
				Quantity: sdk.NewDec(6),
				Allocations: []*types.Allocation{{
					OrderId:  6,
					Account:  "abc",
					Quantity: sdk.NewDec(4),
				}, {
					OrderId:  7,
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
					OrderId:  5,
					Account:  "abc",
					Quantity: sdk.NewDec(2),
				}},
				PriceDenom: "USDC",
				AssetDenom: "ATOM",
			},
		},
	}
	entries := &types.CachedSortedOrderBookEntries{
		Entries:      longBook,
		DirtyEntries: datastructures.NewTypedSyncMap[string, types.OrderBookEntry](),
	}
	outcome := exchange.MatchMarketOrders(
		ctx, shortOrders, entries, types.PositionDirection_SHORT,
	)
	totalPrice := outcome.TotalNotional
	totalExecuted := outcome.TotalQuantity
	settlements := outcome.Settlements
	assert.Equal(t, totalPrice, sdk.NewDec(520))
	assert.Equal(t, totalExecuted, sdk.NewDec(5))
	assert.Equal(t, entries.DirtyEntries.Len(), 2)
	_, ok := entries.DirtyEntries.Load(sdk.MustNewDecFromStr("100").String())
	assert.True(t, ok)
	_, ok = entries.DirtyEntries.Load(sdk.MustNewDecFromStr("110").String())
	assert.True(t, ok)
	assert.Equal(t, len(longBook), 2)
	assert.Equal(t, longBook[0].GetPrice(), sdk.NewDec(100))
	assert.Equal(t, *longBook[0].GetEntry(), types.OrderEntry{
		Price:    sdk.NewDec(100),
		Quantity: sdk.NewDec(3),
		Allocations: []*types.Allocation{{
			OrderId:  6,
			Account:  "abc",
			Quantity: sdk.NewDec(1),
		}, {
			OrderId:  7,
			Account:  "ghi",
			Quantity: sdk.NewDec(2),
		}},
		PriceDenom: "USDC",
		AssetDenom: "ATOM",
	})
	assert.Equal(t, longBook[1].GetPrice(), sdk.NewDec(110))
	assert.Equal(t, longBook[1].GetEntry().Price, sdk.NewDec(110))
	assert.Equal(t, longBook[1].GetEntry().Quantity.IsZero(), true)
	assert.Equal(t, len(settlements), 4)
	assert.Equal(t, *settlements[0], types.SettlementEntry{
		PositionDirection:      "Long",
		PriceDenom:             "USDC",
		AssetDenom:             "ATOM",
		Quantity:               sdk.NewDec(2),
		ExecutionCostOrProceed: sdk.NewDec(110),
		ExpectedCostOrProceed:  sdk.NewDec(110),
		Account:                "abc",
		OrderType:              "Limit",
		OrderId:                5,
		Timestamp:              TestTimestamp,
		Height:                 TestHeight,
	})
	assert.Equal(t, *settlements[1], types.SettlementEntry{
		PositionDirection:      "Long",
		PriceDenom:             "USDC",
		AssetDenom:             "ATOM",
		Quantity:               sdk.NewDec(3),
		ExecutionCostOrProceed: sdk.NewDec(100),
		ExpectedCostOrProceed:  sdk.NewDec(100),
		Account:                "abc",
		OrderType:              "Limit",
		OrderId:                6,
		Timestamp:              TestTimestamp,
		Height:                 TestHeight,
	})
	assert.Equal(t, *settlements[2], types.SettlementEntry{
		PositionDirection:      "Short",
		PriceDenom:             "USDC",
		AssetDenom:             "ATOM",
		Quantity:               sdk.NewDec(2),
		ExecutionCostOrProceed: sdk.NewDec(104),
		ExpectedCostOrProceed:  sdk.NewDec(100),
		Account:                "def",
		OrderType:              "Market",
		OrderId:                1,
		Timestamp:              TestTimestamp,
		Height:                 TestHeight,
	})
	assert.Equal(t, *settlements[3], types.SettlementEntry{
		PositionDirection:      "Short",
		PriceDenom:             "USDC",
		AssetDenom:             "ATOM",
		Quantity:               sdk.NewDec(3),
		ExecutionCostOrProceed: sdk.NewDec(104),
		ExpectedCostOrProceed:  sdk.NewDec(100),
		Account:                "def",
		OrderType:              "Market",
		OrderId:                1,
		Timestamp:              TestTimestamp,
		Height:                 TestHeight,
	})
}

func TestMatchMultipleMarketOrderFromMultipleShortBook(t *testing.T) {
	_, ctx := keepertest.DexKeeper(t)
	ctx = ctx.WithBlockHeight(int64(TestHeight)).WithBlockTime(time.Unix(int64(TestTimestamp), 0))
	longOrders := []*types.Order{
		{
			Id:                1,
			Price:             sdk.NewDec(104),
			Quantity:          sdk.NewDec(1),
			Account:           "jkl",
			PositionDirection: types.PositionDirection_LONG,
			ContractAddr:      "test",
			PriceDenom:        "USDC",
			AssetDenom:        "ATOM",
			OrderType:         types.OrderType_MARKET,
		},
		{
			Id:                2,
			Price:             sdk.NewDec(100),
			Quantity:          sdk.NewDec(5),
			Account:           "abc",
			PositionDirection: types.PositionDirection_LONG,
			ContractAddr:      "test",
			PriceDenom:        "USDC",
			AssetDenom:        "ATOM",
			OrderType:         types.OrderType_MARKET,
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
			OrderType:         types.OrderType_MARKET,
		},
	}
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
	entries := &types.CachedSortedOrderBookEntries{
		Entries:      shortBook,
		DirtyEntries: datastructures.NewTypedSyncMap[string, types.OrderBookEntry](),
	}
	outcome := exchange.MatchMarketOrders(
		ctx, longOrders, entries, types.PositionDirection_LONG,
	)
	totalPrice := outcome.TotalNotional
	totalExecuted := outcome.TotalQuantity
	settlements := outcome.Settlements
	assert.Equal(t, totalPrice, sdk.NewDec(580))
	assert.Equal(t, totalExecuted, sdk.NewDec(6))
	assert.Equal(t, entries.DirtyEntries.Len(), 2)
	_, ok := entries.DirtyEntries.Load(sdk.MustNewDecFromStr("100").String())
	assert.True(t, ok)
	_, ok = entries.DirtyEntries.Load(sdk.MustNewDecFromStr("90").String())
	assert.True(t, ok)
	assert.Equal(t, len(shortBook), 2)
	assert.Equal(t, shortBook[0].GetPrice(), sdk.NewDec(90))
	assert.Equal(t, shortBook[0].GetEntry().Price, sdk.NewDec(90))
	assert.Equal(t, shortBook[0].GetEntry().Quantity.IsZero(), true)
	assert.Equal(t, shortBook[1].GetPrice(), sdk.NewDec(100))
	assert.Equal(t, *shortBook[1].GetEntry(), types.OrderEntry{
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
		PositionDirection:      "Short",
		PriceDenom:             "USDC",
		AssetDenom:             "ATOM",
		Quantity:               sdk.NewDec(1),
		ExecutionCostOrProceed: sdk.NewDec(90),
		ExpectedCostOrProceed:  sdk.NewDec(90),
		Account:                "def",
		OrderType:              "Limit",
		OrderId:                4,
		Timestamp:              TestTimestamp,
		Height:                 TestHeight,
	})
	assert.Equal(t, *settlements[1], types.SettlementEntry{
		PositionDirection:      "Short",
		PriceDenom:             "USDC",
		AssetDenom:             "ATOM",
		Quantity:               sdk.NewDec(1),
		ExecutionCostOrProceed: sdk.NewDec(90),
		ExpectedCostOrProceed:  sdk.NewDec(90),
		Account:                "def",
		OrderType:              "Limit",
		OrderId:                4,
		Timestamp:              TestTimestamp,
		Height:                 TestHeight,
	})
	assert.Equal(t, *settlements[2], types.SettlementEntry{
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
	assert.Equal(t, *settlements[3], types.SettlementEntry{
		PositionDirection:      "Long",
		PriceDenom:             "USDC",
		AssetDenom:             "ATOM",
		Quantity:               sdk.NewDec(1),
		ExecutionCostOrProceed: sdk.MustNewDecFromStr("96.666666666666666667"),
		ExpectedCostOrProceed:  sdk.NewDec(104),
		Account:                "jkl",
		OrderType:              "Market",
		OrderId:                1,
		Timestamp:              TestTimestamp,
		Height:                 TestHeight,
	})
	assert.Equal(t, *settlements[4], types.SettlementEntry{
		PositionDirection:      "Long",
		PriceDenom:             "USDC",
		AssetDenom:             "ATOM",
		Quantity:               sdk.NewDec(1),
		ExecutionCostOrProceed: sdk.MustNewDecFromStr("96.666666666666666667"),
		ExpectedCostOrProceed:  sdk.NewDec(100),
		Account:                "abc",
		OrderType:              "Market",
		OrderId:                2,
		Timestamp:              TestTimestamp,
		Height:                 TestHeight,
	})
	assert.Equal(t, *settlements[5], types.SettlementEntry{
		PositionDirection:      "Long",
		PriceDenom:             "USDC",
		AssetDenom:             "ATOM",
		Quantity:               sdk.NewDec(4),
		ExecutionCostOrProceed: sdk.MustNewDecFromStr("96.666666666666666667"),
		ExpectedCostOrProceed:  sdk.NewDec(100),
		Account:                "abc",
		OrderType:              "Market",
		OrderId:                2,
		Timestamp:              TestTimestamp,
		Height:                 TestHeight,
	})
}

func TestMatchMultipleMarketOrderFromMultipleLongBook(t *testing.T) {
	_, ctx := keepertest.DexKeeper(t)
	ctx = ctx.WithBlockHeight(int64(TestHeight)).WithBlockTime(time.Unix(int64(TestTimestamp), 0))
	shortOrders := []*types.Order{
		{
			Id:                1,
			Price:             sdk.NewDec(96),
			Quantity:          sdk.NewDec(1),
			Account:           "jkl",
			PositionDirection: types.PositionDirection_SHORT,
			ContractAddr:      "test",
			PriceDenom:        "USDC",
			AssetDenom:        "ATOM",
			OrderType:         types.OrderType_MARKET,
		},
		{
			Id:                2,
			Price:             sdk.NewDec(100),
			Quantity:          sdk.NewDec(5),
			Account:           "abc",
			PositionDirection: types.PositionDirection_SHORT,
			ContractAddr:      "test",
			PriceDenom:        "USDC",
			AssetDenom:        "ATOM",
			OrderType:         types.OrderType_MARKET,
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
			OrderType:         types.OrderType_MARKET,
		},
	}
	longBook := []types.OrderBookEntry{
		&types.LongBook{
			Price: sdk.NewDec(100),
			Entry: &types.OrderEntry{
				Price:    sdk.NewDec(100),
				Quantity: sdk.NewDec(6),
				Allocations: []*types.Allocation{{
					OrderId:  5,
					Account:  "abc",
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
	entries := &types.CachedSortedOrderBookEntries{
		Entries:      longBook,
		DirtyEntries: datastructures.NewTypedSyncMap[string, types.OrderBookEntry](),
	}
	outcome := exchange.MatchMarketOrders(
		ctx, shortOrders, entries, types.PositionDirection_SHORT,
	)
	totalPrice := outcome.TotalNotional
	totalExecuted := outcome.TotalQuantity
	settlements := outcome.Settlements
	assert.Equal(t, totalPrice, sdk.NewDec(620))
	assert.Equal(t, totalExecuted, sdk.NewDec(6))
	assert.Equal(t, entries.DirtyEntries.Len(), 2)
	_, ok := entries.DirtyEntries.Load(sdk.MustNewDecFromStr("100").String())
	assert.True(t, ok)
	_, ok = entries.DirtyEntries.Load(sdk.MustNewDecFromStr("110").String())
	assert.True(t, ok)
	assert.Equal(t, len(longBook), 2)
	assert.Equal(t, longBook[0].GetPrice(), sdk.NewDec(100))
	assert.Equal(t, *longBook[0].GetEntry(), types.OrderEntry{
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
	assert.Equal(t, longBook[1].GetPrice(), sdk.NewDec(110))
	assert.Equal(t, longBook[1].GetEntry().Price, sdk.NewDec(110))
	assert.Equal(t, longBook[1].GetEntry().Quantity.IsZero(), true)
	assert.Equal(t, len(settlements), 6)
	assert.Equal(t, *settlements[0], types.SettlementEntry{
		PositionDirection:      "Long",
		PriceDenom:             "USDC",
		AssetDenom:             "ATOM",
		Quantity:               sdk.NewDec(1),
		ExecutionCostOrProceed: sdk.NewDec(110),
		ExpectedCostOrProceed:  sdk.NewDec(110),
		Account:                "abc",
		OrderType:              "Limit",
		OrderId:                4,
		Timestamp:              TestTimestamp,
		Height:                 TestHeight,
	})
	assert.Equal(t, *settlements[1], types.SettlementEntry{
		PositionDirection:      "Long",
		PriceDenom:             "USDC",
		AssetDenom:             "ATOM",
		Quantity:               sdk.NewDec(1),
		ExecutionCostOrProceed: sdk.NewDec(110),
		ExpectedCostOrProceed:  sdk.NewDec(110),
		Account:                "abc",
		OrderType:              "Limit",
		OrderId:                4,
		Timestamp:              TestTimestamp,
		Height:                 TestHeight,
	})
	assert.Equal(t, *settlements[2], types.SettlementEntry{
		PositionDirection:      "Long",
		PriceDenom:             "USDC",
		AssetDenom:             "ATOM",
		Quantity:               sdk.NewDec(4),
		ExecutionCostOrProceed: sdk.NewDec(100),
		ExpectedCostOrProceed:  sdk.NewDec(100),
		Account:                "abc",
		OrderType:              "Limit",
		OrderId:                5,
		Timestamp:              TestTimestamp,
		Height:                 TestHeight,
	})
	assert.Equal(t, *settlements[3], types.SettlementEntry{
		PositionDirection:      "Short",
		PriceDenom:             "USDC",
		AssetDenom:             "ATOM",
		Quantity:               sdk.NewDec(1),
		ExecutionCostOrProceed: sdk.MustNewDecFromStr("103.333333333333333333"),
		ExpectedCostOrProceed:  sdk.NewDec(96),
		Account:                "jkl",
		OrderType:              "Market",
		OrderId:                1,
		Timestamp:              TestTimestamp,
		Height:                 TestHeight,
	})
	assert.Equal(t, *settlements[4], types.SettlementEntry{
		PositionDirection:      "Short",
		PriceDenom:             "USDC",
		AssetDenom:             "ATOM",
		Quantity:               sdk.NewDec(1),
		ExecutionCostOrProceed: sdk.MustNewDecFromStr("103.333333333333333333"),
		ExpectedCostOrProceed:  sdk.NewDec(100),
		Account:                "abc",
		OrderType:              "Market",
		OrderId:                2,
		Timestamp:              TestTimestamp,
		Height:                 TestHeight,
	})
	assert.Equal(t, *settlements[5], types.SettlementEntry{
		PositionDirection:      "Short",
		PriceDenom:             "USDC",
		AssetDenom:             "ATOM",
		Quantity:               sdk.NewDec(4),
		ExecutionCostOrProceed: sdk.MustNewDecFromStr("103.333333333333333333"),
		ExpectedCostOrProceed:  sdk.NewDec(100),
		Account:                "abc",
		OrderType:              "Market",
		OrderId:                2,
		Timestamp:              TestTimestamp,
		Height:                 TestHeight,
	})
}
