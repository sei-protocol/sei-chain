package exchange_test

import (
	"testing"

	sdk "github.com/cosmos/cosmos-sdk/types"
	dexcache "github.com/sei-protocol/sei-chain/x/dex/cache"
	"github.com/sei-protocol/sei-chain/x/dex/exchange"
	"github.com/sei-protocol/sei-chain/x/dex/types"
	"github.com/stretchr/testify/assert"

	keepertest "github.com/sei-protocol/sei-chain/testutil/keeper"
)

func TestMatchSingleMarketOrderFromShortBook(t *testing.T) {
	_, ctx := keepertest.DexKeeper(t)
	longOrders := []dexcache.MarketOrder{
		{
			WorstPrice: sdk.NewDec(100),
			Quantity:   sdk.NewDec(5),
			Creator:    "abc",
			Direction:  types.PositionDirection_LONG,
		},
	}
	shortBook := []types.OrderBook{
		&types.ShortBook{
			Price: sdk.NewDec(100),
			Entry: &types.OrderEntry{
				Price:             sdk.NewDec(100),
				Quantity:          sdk.NewDec(5),
				AllocationCreator: []string{"def|c|"},
				Allocation:        []sdk.Dec{sdk.NewDec(5)},
				PriceDenom:        TEST_PAIR().PriceDenom,
				AssetDenom:        TEST_PAIR().AssetDenom,
			},
		},
	}
	dirtyPrices := exchange.NewDirtyPrices()
	settlements := []*types.Settlement{}
	totalPrice, totalExecuted := exchange.MatchMarketOrders(
		ctx, longOrders, shortBook, TEST_PAIR(), types.PositionDirection_LONG, &dirtyPrices, &settlements,
	)
	assert.Equal(t, totalPrice, sdk.NewDec(500))
	assert.Equal(t, totalExecuted, sdk.NewDec(5))
	assert.Equal(t, len(dirtyPrices.Get()), 1)
	assert.Equal(t, dirtyPrices.Has(sdk.NewDec(100)), true)
	assert.Equal(t, len(shortBook), 1)
	assert.Equal(t, shortBook[0].GetPrice(), sdk.NewDec(100))
	assert.Equal(t, shortBook[0].GetEntry().Price, sdk.NewDec(100))
	assert.Equal(t, shortBook[0].GetEntry().Quantity.IsZero(), true)
	assert.Equal(t, len(settlements), 2)
	assert.Equal(t, *settlements[0], types.Settlement{
		Direction:              types.PositionDirection_SHORT,
		PriceSymbol:            TEST_PAIR().PriceDenom,
		AssetSymbol:            TEST_PAIR().AssetDenom,
		Quantity:               sdk.NewDec(5),
		ExecutionCostOrProceed: sdk.NewDec(100),
		ExpectedCostOrProceed:  sdk.NewDec(100),
		Account:                "def",
		OrderType:              types.OrderType_LIMIT,
	})
	assert.Equal(t, *settlements[1], types.Settlement{
		Direction:              types.PositionDirection_LONG,
		PriceSymbol:            TEST_PAIR().PriceDenom,
		AssetSymbol:            TEST_PAIR().AssetDenom,
		Quantity:               sdk.NewDec(5),
		ExecutionCostOrProceed: sdk.NewDec(100),
		ExpectedCostOrProceed:  sdk.NewDec(100),
		Account:                "abc",
		OrderType:              types.OrderType_MARKET,
	})
}

func TestMatchSingleMarketOrderFromLongBook(t *testing.T) {
	_, ctx := keepertest.DexKeeper(t)
	shortOrders := []dexcache.MarketOrder{
		{
			WorstPrice: sdk.NewDec(100),
			Quantity:   sdk.NewDec(5),
			Creator:    "abc",
			Direction:  types.PositionDirection_SHORT,
		},
	}
	longBook := []types.OrderBook{
		&types.LongBook{
			Price: sdk.NewDec(100),
			Entry: &types.OrderEntry{
				Price:             sdk.NewDec(100),
				Quantity:          sdk.NewDec(5),
				AllocationCreator: []string{"def|c|"},
				Allocation:        []sdk.Dec{sdk.NewDec(5)},
				PriceDenom:        TEST_PAIR().PriceDenom,
				AssetDenom:        TEST_PAIR().AssetDenom,
			},
		},
	}
	dirtyPrices := exchange.NewDirtyPrices()
	settlements := []*types.Settlement{}
	totalPrice, totalExecuted := exchange.MatchMarketOrders(
		ctx, shortOrders, longBook, TEST_PAIR(), types.PositionDirection_SHORT, &dirtyPrices, &settlements,
	)
	assert.Equal(t, totalPrice, sdk.NewDec(500))
	assert.Equal(t, totalExecuted, sdk.NewDec(5))
	assert.Equal(t, len(dirtyPrices.Get()), 1)
	assert.Equal(t, dirtyPrices.Has(sdk.NewDec(100)), true)
	assert.Equal(t, len(longBook), 1)
	assert.Equal(t, longBook[0].GetPrice(), sdk.NewDec(100))
	assert.Equal(t, longBook[0].GetEntry().Price, sdk.NewDec(100))
	assert.Equal(t, longBook[0].GetEntry().Quantity.IsZero(), true)
	assert.Equal(t, len(settlements), 2)
	assert.Equal(t, *settlements[0], types.Settlement{
		Direction:              types.PositionDirection_LONG,
		PriceSymbol:            TEST_PAIR().PriceDenom,
		AssetSymbol:            TEST_PAIR().AssetDenom,
		Quantity:               sdk.NewDec(5),
		ExecutionCostOrProceed: sdk.NewDec(100),
		ExpectedCostOrProceed:  sdk.NewDec(100),
		Account:                "def",
		OrderType:              types.OrderType_LIMIT,
	})
	assert.Equal(t, *settlements[1], types.Settlement{
		Direction:              types.PositionDirection_SHORT,
		PriceSymbol:            TEST_PAIR().PriceDenom,
		AssetSymbol:            TEST_PAIR().AssetDenom,
		Quantity:               sdk.NewDec(5),
		ExecutionCostOrProceed: sdk.NewDec(100),
		ExpectedCostOrProceed:  sdk.NewDec(100),
		Account:                "abc",
		OrderType:              types.OrderType_MARKET,
	})
}

func TestMatchSingleMarketOrderFromMultipleShortBook(t *testing.T) {
	_, ctx := keepertest.DexKeeper(t)
	longOrders := []dexcache.MarketOrder{
		{
			WorstPrice: sdk.NewDec(100),
			Quantity:   sdk.NewDec(5),
			Creator:    "abc",
			Direction:  types.PositionDirection_LONG,
		},
	}
	shortBook := []types.OrderBook{
		&types.ShortBook{
			Price: sdk.NewDec(90),
			Entry: &types.OrderEntry{
				Price:             sdk.NewDec(90),
				Quantity:          sdk.NewDec(2),
				AllocationCreator: []string{"def|c|"},
				Allocation:        []sdk.Dec{sdk.NewDec(2)},
				PriceDenom:        TEST_PAIR().PriceDenom,
				AssetDenom:        TEST_PAIR().AssetDenom,
			},
		},
		&types.ShortBook{
			Price: sdk.NewDec(100),
			Entry: &types.OrderEntry{
				Price:             sdk.NewDec(100),
				Quantity:          sdk.NewDec(6),
				AllocationCreator: []string{"def|c|", "ghi|c|"},
				Allocation:        []sdk.Dec{sdk.NewDec(4), sdk.NewDec(2)},
				PriceDenom:        TEST_PAIR().PriceDenom,
				AssetDenom:        TEST_PAIR().AssetDenom,
			},
		},
	}
	dirtyPrices := exchange.NewDirtyPrices()
	settlements := []*types.Settlement{}
	totalPrice, totalExecuted := exchange.MatchMarketOrders(
		ctx, longOrders, shortBook, TEST_PAIR(), types.PositionDirection_LONG, &dirtyPrices, &settlements,
	)
	assert.Equal(t, totalPrice, sdk.NewDec(480))
	assert.Equal(t, totalExecuted, sdk.NewDec(5))
	assert.Equal(t, len(dirtyPrices.Get()), 2)
	assert.Equal(t, dirtyPrices.Has(sdk.NewDec(90)), true)
	assert.Equal(t, dirtyPrices.Has(sdk.NewDec(100)), true)
	assert.Equal(t, len(shortBook), 2)
	assert.Equal(t, shortBook[0].GetPrice(), sdk.NewDec(90))
	assert.Equal(t, shortBook[0].GetEntry().Price, sdk.NewDec(90))
	assert.Equal(t, shortBook[0].GetEntry().Quantity.IsZero(), true)
	assert.Equal(t, shortBook[1].GetPrice(), sdk.NewDec(100))
	assert.Equal(t, *shortBook[1].GetEntry(), types.OrderEntry{
		Price:             sdk.NewDec(100),
		Quantity:          sdk.NewDec(3),
		AllocationCreator: []string{"def|c|", "ghi|c|"},
		Allocation:        []sdk.Dec{sdk.NewDec(2), sdk.NewDec(1)},
		PriceDenom:        TEST_PAIR().PriceDenom,
		AssetDenom:        TEST_PAIR().AssetDenom,
	})
	assert.Equal(t, len(settlements), 6)
	assert.Equal(t, *settlements[0], types.Settlement{
		Direction:              types.PositionDirection_SHORT,
		PriceSymbol:            TEST_PAIR().PriceDenom,
		AssetSymbol:            TEST_PAIR().AssetDenom,
		Quantity:               sdk.NewDec(2),
		ExecutionCostOrProceed: sdk.NewDec(90),
		ExpectedCostOrProceed:  sdk.NewDec(90),
		Account:                "def",
		OrderType:              types.OrderType_LIMIT,
	})
	assert.Equal(t, *settlements[1], types.Settlement{
		Direction:              types.PositionDirection_SHORT,
		PriceSymbol:            TEST_PAIR().PriceDenom,
		AssetSymbol:            TEST_PAIR().AssetDenom,
		Quantity:               sdk.NewDec(2),
		ExecutionCostOrProceed: sdk.NewDec(100),
		ExpectedCostOrProceed:  sdk.NewDec(100),
		Account:                "def",
		OrderType:              types.OrderType_LIMIT,
	})
	assert.Equal(t, *settlements[2], types.Settlement{
		Direction:              types.PositionDirection_SHORT,
		PriceSymbol:            TEST_PAIR().PriceDenom,
		AssetSymbol:            TEST_PAIR().AssetDenom,
		Quantity:               sdk.NewDec(1),
		ExecutionCostOrProceed: sdk.NewDec(100),
		ExpectedCostOrProceed:  sdk.NewDec(100),
		Account:                "ghi",
		OrderType:              types.OrderType_LIMIT,
	})
	assert.Equal(t, *settlements[3], types.Settlement{
		Direction:              types.PositionDirection_LONG,
		PriceSymbol:            TEST_PAIR().PriceDenom,
		AssetSymbol:            TEST_PAIR().AssetDenom,
		Quantity:               sdk.NewDec(2),
		ExecutionCostOrProceed: sdk.NewDec(96),
		ExpectedCostOrProceed:  sdk.NewDec(100),
		Account:                "abc",
		OrderType:              types.OrderType_MARKET,
	})
	assert.Equal(t, *settlements[4], types.Settlement{
		Direction:              types.PositionDirection_LONG,
		PriceSymbol:            TEST_PAIR().PriceDenom,
		AssetSymbol:            TEST_PAIR().AssetDenom,
		Quantity:               sdk.NewDec(2),
		ExecutionCostOrProceed: sdk.NewDec(96),
		ExpectedCostOrProceed:  sdk.NewDec(100),
		Account:                "abc",
		OrderType:              types.OrderType_MARKET,
	})
	assert.Equal(t, *settlements[5], types.Settlement{
		Direction:              types.PositionDirection_LONG,
		PriceSymbol:            TEST_PAIR().PriceDenom,
		AssetSymbol:            TEST_PAIR().AssetDenom,
		Quantity:               sdk.NewDec(1),
		ExecutionCostOrProceed: sdk.NewDec(96),
		ExpectedCostOrProceed:  sdk.NewDec(100),
		Account:                "abc",
		OrderType:              types.OrderType_MARKET,
	})
}

func TestMatchSingleMarketOrderFromMultipleLongBook(t *testing.T) {
	_, ctx := keepertest.DexKeeper(t)
	shortOrders := []dexcache.MarketOrder{
		{
			WorstPrice: sdk.NewDec(100),
			Quantity:   sdk.NewDec(5),
			Creator:    "def",
			Direction:  types.PositionDirection_SHORT,
		},
	}
	longBook := []types.OrderBook{
		&types.LongBook{
			Price: sdk.NewDec(100),
			Entry: &types.OrderEntry{
				Price:             sdk.NewDec(100),
				Quantity:          sdk.NewDec(6),
				AllocationCreator: []string{"abc|c|", "ghi|c|"},
				Allocation:        []sdk.Dec{sdk.NewDec(4), sdk.NewDec(2)},
				PriceDenom:        TEST_PAIR().PriceDenom,
				AssetDenom:        TEST_PAIR().AssetDenom,
			},
		},
		&types.LongBook{
			Price: sdk.NewDec(110),
			Entry: &types.OrderEntry{
				Price:             sdk.NewDec(110),
				Quantity:          sdk.NewDec(2),
				AllocationCreator: []string{"abc|c|"},
				Allocation:        []sdk.Dec{sdk.NewDec(2)},
				PriceDenom:        TEST_PAIR().PriceDenom,
				AssetDenom:        TEST_PAIR().AssetDenom,
			},
		},
	}
	dirtyPrices := exchange.NewDirtyPrices()
	settlements := []*types.Settlement{}
	totalPrice, totalExecuted := exchange.MatchMarketOrders(
		ctx, shortOrders, longBook, TEST_PAIR(), types.PositionDirection_SHORT, &dirtyPrices, &settlements,
	)
	assert.Equal(t, totalPrice, sdk.NewDec(520))
	assert.Equal(t, totalExecuted, sdk.NewDec(5))
	assert.Equal(t, len(dirtyPrices.Get()), 2)
	assert.Equal(t, dirtyPrices.Has(sdk.NewDec(100)), true)
	assert.Equal(t, dirtyPrices.Has(sdk.NewDec(110)), true)
	assert.Equal(t, len(longBook), 2)
	assert.Equal(t, longBook[0].GetPrice(), sdk.NewDec(100))
	assert.Equal(t, *longBook[0].GetEntry(), types.OrderEntry{
		Price:             sdk.NewDec(100),
		Quantity:          sdk.NewDec(3),
		AllocationCreator: []string{"abc|c|", "ghi|c|"},
		Allocation:        []sdk.Dec{sdk.NewDec(2), sdk.NewDec(1)},
		PriceDenom:        TEST_PAIR().PriceDenom,
		AssetDenom:        TEST_PAIR().AssetDenom,
	})
	assert.Equal(t, longBook[1].GetPrice(), sdk.NewDec(110))
	assert.Equal(t, longBook[1].GetEntry().Price, sdk.NewDec(110))
	assert.Equal(t, longBook[1].GetEntry().Quantity.IsZero(), true)
	assert.Equal(t, len(settlements), 6)
	assert.Equal(t, *settlements[0], types.Settlement{
		Direction:              types.PositionDirection_LONG,
		PriceSymbol:            TEST_PAIR().PriceDenom,
		AssetSymbol:            TEST_PAIR().AssetDenom,
		Quantity:               sdk.NewDec(2),
		ExecutionCostOrProceed: sdk.NewDec(110),
		ExpectedCostOrProceed:  sdk.NewDec(110),
		Account:                "abc",
		OrderType:              types.OrderType_LIMIT,
	})
	assert.Equal(t, *settlements[1], types.Settlement{
		Direction:              types.PositionDirection_LONG,
		PriceSymbol:            TEST_PAIR().PriceDenom,
		AssetSymbol:            TEST_PAIR().AssetDenom,
		Quantity:               sdk.NewDec(2),
		ExecutionCostOrProceed: sdk.NewDec(100),
		ExpectedCostOrProceed:  sdk.NewDec(100),
		Account:                "abc",
		OrderType:              types.OrderType_LIMIT,
	})
	assert.Equal(t, *settlements[2], types.Settlement{
		Direction:              types.PositionDirection_LONG,
		PriceSymbol:            TEST_PAIR().PriceDenom,
		AssetSymbol:            TEST_PAIR().AssetDenom,
		Quantity:               sdk.NewDec(1),
		ExecutionCostOrProceed: sdk.NewDec(100),
		ExpectedCostOrProceed:  sdk.NewDec(100),
		Account:                "ghi",
		OrderType:              types.OrderType_LIMIT,
	})
	assert.Equal(t, *settlements[3], types.Settlement{
		Direction:              types.PositionDirection_SHORT,
		PriceSymbol:            TEST_PAIR().PriceDenom,
		AssetSymbol:            TEST_PAIR().AssetDenom,
		Quantity:               sdk.NewDec(2),
		ExecutionCostOrProceed: sdk.NewDec(104),
		ExpectedCostOrProceed:  sdk.NewDec(100),
		Account:                "def",
		OrderType:              types.OrderType_MARKET,
	})
	assert.Equal(t, *settlements[4], types.Settlement{
		Direction:              types.PositionDirection_SHORT,
		PriceSymbol:            TEST_PAIR().PriceDenom,
		AssetSymbol:            TEST_PAIR().AssetDenom,
		Quantity:               sdk.NewDec(2),
		ExecutionCostOrProceed: sdk.NewDec(104),
		ExpectedCostOrProceed:  sdk.NewDec(100),
		Account:                "def",
		OrderType:              types.OrderType_MARKET,
	})
	assert.Equal(t, *settlements[5], types.Settlement{
		Direction:              types.PositionDirection_SHORT,
		PriceSymbol:            TEST_PAIR().PriceDenom,
		AssetSymbol:            TEST_PAIR().AssetDenom,
		Quantity:               sdk.NewDec(1),
		ExecutionCostOrProceed: sdk.NewDec(104),
		ExpectedCostOrProceed:  sdk.NewDec(100),
		Account:                "def",
		OrderType:              types.OrderType_MARKET,
	})
}

func TestMatchMultipleMarketOrderFromMultipleShortBook(t *testing.T) {
	_, ctx := keepertest.DexKeeper(t)
	longOrders := []dexcache.MarketOrder{
		{
			WorstPrice: sdk.NewDec(104),
			Quantity:   sdk.NewDec(1),
			Creator:    "jkl",
			Direction:  types.PositionDirection_LONG,
		},
		{
			WorstPrice: sdk.NewDec(100),
			Quantity:   sdk.NewDec(5),
			Creator:    "abc",
			Direction:  types.PositionDirection_LONG,
		},
		{
			WorstPrice: sdk.NewDec(98),
			Quantity:   sdk.NewDec(2),
			Creator:    "mno",
			Direction:  types.PositionDirection_LONG,
		},
	}
	shortBook := []types.OrderBook{
		&types.ShortBook{
			Price: sdk.NewDec(90),
			Entry: &types.OrderEntry{
				Price:             sdk.NewDec(90),
				Quantity:          sdk.NewDec(2),
				AllocationCreator: []string{"def|c|"},
				Allocation:        []sdk.Dec{sdk.NewDec(2)},
				PriceDenom:        TEST_PAIR().PriceDenom,
				AssetDenom:        TEST_PAIR().AssetDenom,
			},
		},
		&types.ShortBook{
			Price: sdk.NewDec(100),
			Entry: &types.OrderEntry{
				Price:             sdk.NewDec(100),
				Quantity:          sdk.NewDec(6),
				AllocationCreator: []string{"def|c|", "ghi|c|"},
				Allocation:        []sdk.Dec{sdk.NewDec(4), sdk.NewDec(2)},
				PriceDenom:        TEST_PAIR().PriceDenom,
				AssetDenom:        TEST_PAIR().AssetDenom,
			},
		},
	}
	dirtyPrices := exchange.NewDirtyPrices()
	settlements := []*types.Settlement{}
	totalPrice, totalExecuted := exchange.MatchMarketOrders(
		ctx, longOrders, shortBook, TEST_PAIR(), types.PositionDirection_LONG, &dirtyPrices, &settlements,
	)
	assert.Equal(t, totalPrice, sdk.NewDec(580))
	assert.Equal(t, totalExecuted, sdk.NewDec(6))
	assert.Equal(t, len(dirtyPrices.Get()), 2)
	assert.Equal(t, dirtyPrices.Has(sdk.NewDec(90)), true)
	assert.Equal(t, dirtyPrices.Has(sdk.NewDec(100)), true)
	assert.Equal(t, len(shortBook), 2)
	assert.Equal(t, shortBook[0].GetPrice(), sdk.NewDec(90))
	assert.Equal(t, shortBook[0].GetEntry().Price, sdk.NewDec(90))
	assert.Equal(t, shortBook[0].GetEntry().Quantity.IsZero(), true)
	assert.Equal(t, shortBook[1].GetPrice(), sdk.NewDec(100))
	assert.Equal(t, *shortBook[1].GetEntry(), types.OrderEntry{
		Price:             sdk.NewDec(100),
		Quantity:          sdk.NewDec(2),
		AllocationCreator: []string{"def|c|", "ghi|c|"},
		Allocation:        []sdk.Dec{sdk.NewDec(4).Quo(sdk.NewDec(3)), sdk.NewDec(2).Quo(sdk.NewDec(3))},
		PriceDenom:        TEST_PAIR().PriceDenom,
		AssetDenom:        TEST_PAIR().AssetDenom,
	})
	assert.Equal(t, len(settlements), 8)
	assert.Equal(t, *settlements[0], types.Settlement{
		Direction:              types.PositionDirection_SHORT,
		PriceSymbol:            TEST_PAIR().PriceDenom,
		AssetSymbol:            TEST_PAIR().AssetDenom,
		Quantity:               sdk.NewDec(1),
		ExecutionCostOrProceed: sdk.NewDec(90),
		ExpectedCostOrProceed:  sdk.NewDec(90),
		Account:                "def",
		OrderType:              types.OrderType_LIMIT,
	})
	assert.Equal(t, *settlements[1], types.Settlement{
		Direction:              types.PositionDirection_SHORT,
		PriceSymbol:            TEST_PAIR().PriceDenom,
		AssetSymbol:            TEST_PAIR().AssetDenom,
		Quantity:               sdk.NewDec(1),
		ExecutionCostOrProceed: sdk.NewDec(90),
		ExpectedCostOrProceed:  sdk.NewDec(90),
		Account:                "def",
		OrderType:              types.OrderType_LIMIT,
	})
	assert.Equal(t, *settlements[2], types.Settlement{
		Direction:              types.PositionDirection_SHORT,
		PriceSymbol:            TEST_PAIR().PriceDenom,
		AssetSymbol:            TEST_PAIR().AssetDenom,
		Quantity:               sdk.NewDec(8).Quo(sdk.NewDec(3)),
		ExecutionCostOrProceed: sdk.NewDec(100),
		ExpectedCostOrProceed:  sdk.NewDec(100),
		Account:                "def",
		OrderType:              types.OrderType_LIMIT,
	})
	assert.Equal(t, *settlements[3], types.Settlement{
		Direction:              types.PositionDirection_SHORT,
		PriceSymbol:            TEST_PAIR().PriceDenom,
		AssetSymbol:            TEST_PAIR().AssetDenom,
		Quantity:               sdk.NewDec(4).Quo(sdk.NewDec(3)),
		ExecutionCostOrProceed: sdk.NewDec(100),
		ExpectedCostOrProceed:  sdk.NewDec(100),
		Account:                "ghi",
		OrderType:              types.OrderType_LIMIT,
	})
	assert.Equal(t, *settlements[4], types.Settlement{
		Direction:              types.PositionDirection_LONG,
		PriceSymbol:            TEST_PAIR().PriceDenom,
		AssetSymbol:            TEST_PAIR().AssetDenom,
		Quantity:               sdk.NewDec(1),
		ExecutionCostOrProceed: sdk.MustNewDecFromStr("96.666666666666666667"),
		ExpectedCostOrProceed:  sdk.NewDec(104),
		Account:                "jkl",
		OrderType:              types.OrderType_MARKET,
	})
	assert.Equal(t, *settlements[5], types.Settlement{
		Direction:              types.PositionDirection_LONG,
		PriceSymbol:            TEST_PAIR().PriceDenom,
		AssetSymbol:            TEST_PAIR().AssetDenom,
		Quantity:               sdk.NewDec(1),
		ExecutionCostOrProceed: sdk.MustNewDecFromStr("96.666666666666666667"),
		ExpectedCostOrProceed:  sdk.NewDec(100),
		Account:                "abc",
		OrderType:              types.OrderType_MARKET,
	})
	assert.Equal(t, *settlements[6], types.Settlement{
		Direction:              types.PositionDirection_LONG,
		PriceSymbol:            TEST_PAIR().PriceDenom,
		AssetSymbol:            TEST_PAIR().AssetDenom,
		Quantity:               sdk.NewDec(8).Quo(sdk.NewDec(3)),
		ExecutionCostOrProceed: sdk.MustNewDecFromStr("96.666666666666666667"),
		ExpectedCostOrProceed:  sdk.NewDec(100),
		Account:                "abc",
		OrderType:              types.OrderType_MARKET,
	})
	assert.Equal(t, *settlements[7], types.Settlement{
		Direction:              types.PositionDirection_LONG,
		PriceSymbol:            TEST_PAIR().PriceDenom,
		AssetSymbol:            TEST_PAIR().AssetDenom,
		Quantity:               sdk.NewDec(4).Quo(sdk.NewDec(3)),
		ExecutionCostOrProceed: sdk.MustNewDecFromStr("96.666666666666666667"),
		ExpectedCostOrProceed:  sdk.NewDec(100),
		Account:                "abc",
		OrderType:              types.OrderType_MARKET,
	})
}

func TestMatchMultipleMarketOrderFromMultipleLongBook(t *testing.T) {
	_, ctx := keepertest.DexKeeper(t)
	shortOrders := []dexcache.MarketOrder{
		{
			WorstPrice: sdk.NewDec(96),
			Quantity:   sdk.NewDec(1),
			Creator:    "jkl",
			Direction:  types.PositionDirection_SHORT,
		},
		{
			WorstPrice: sdk.NewDec(100),
			Quantity:   sdk.NewDec(5),
			Creator:    "abc",
			Direction:  types.PositionDirection_SHORT,
		},
		{
			WorstPrice: sdk.NewDec(102),
			Quantity:   sdk.NewDec(2),
			Creator:    "mno",
			Direction:  types.PositionDirection_SHORT,
		},
	}
	longBook := []types.OrderBook{
		&types.LongBook{
			Price: sdk.NewDec(100),
			Entry: &types.OrderEntry{
				Price:             sdk.NewDec(100),
				Quantity:          sdk.NewDec(6),
				AllocationCreator: []string{"abc|c|", "ghi|c|"},
				Allocation:        []sdk.Dec{sdk.NewDec(4), sdk.NewDec(2)},
				PriceDenom:        TEST_PAIR().PriceDenom,
				AssetDenom:        TEST_PAIR().AssetDenom,
			},
		},
		&types.LongBook{
			Price: sdk.NewDec(110),
			Entry: &types.OrderEntry{
				Price:             sdk.NewDec(110),
				Quantity:          sdk.NewDec(2),
				AllocationCreator: []string{"abc|c|"},
				Allocation:        []sdk.Dec{sdk.NewDec(2)},
				PriceDenom:        TEST_PAIR().PriceDenom,
				AssetDenom:        TEST_PAIR().AssetDenom,
			},
		},
	}
	dirtyPrices := exchange.NewDirtyPrices()
	settlements := []*types.Settlement{}
	totalPrice, totalExecuted := exchange.MatchMarketOrders(
		ctx, shortOrders, longBook, TEST_PAIR(), types.PositionDirection_SHORT, &dirtyPrices, &settlements,
	)
	assert.Equal(t, totalPrice, sdk.NewDec(620))
	assert.Equal(t, totalExecuted, sdk.NewDec(6))
	assert.Equal(t, len(dirtyPrices.Get()), 2)
	assert.Equal(t, dirtyPrices.Has(sdk.NewDec(100)), true)
	assert.Equal(t, dirtyPrices.Has(sdk.NewDec(110)), true)
	assert.Equal(t, len(longBook), 2)
	assert.Equal(t, longBook[0].GetPrice(), sdk.NewDec(100))
	assert.Equal(t, *longBook[0].GetEntry(), types.OrderEntry{
		Price:             sdk.NewDec(100),
		Quantity:          sdk.NewDec(2),
		AllocationCreator: []string{"abc|c|", "ghi|c|"},
		Allocation:        []sdk.Dec{sdk.NewDec(4).Quo(sdk.NewDec(3)), sdk.NewDec(2).Quo(sdk.NewDec(3))},
		PriceDenom:        TEST_PAIR().PriceDenom,
		AssetDenom:        TEST_PAIR().AssetDenom,
	})
	assert.Equal(t, longBook[1].GetPrice(), sdk.NewDec(110))
	assert.Equal(t, longBook[1].GetEntry().Price, sdk.NewDec(110))
	assert.Equal(t, longBook[1].GetEntry().Quantity.IsZero(), true)
	assert.Equal(t, len(settlements), 8)
	assert.Equal(t, *settlements[0], types.Settlement{
		Direction:              types.PositionDirection_LONG,
		PriceSymbol:            TEST_PAIR().PriceDenom,
		AssetSymbol:            TEST_PAIR().AssetDenom,
		Quantity:               sdk.NewDec(1),
		ExecutionCostOrProceed: sdk.NewDec(110),
		ExpectedCostOrProceed:  sdk.NewDec(110),
		Account:                "abc",
		OrderType:              types.OrderType_LIMIT,
	})
	assert.Equal(t, *settlements[1], types.Settlement{
		Direction:              types.PositionDirection_LONG,
		PriceSymbol:            TEST_PAIR().PriceDenom,
		AssetSymbol:            TEST_PAIR().AssetDenom,
		Quantity:               sdk.NewDec(1),
		ExecutionCostOrProceed: sdk.NewDec(110),
		ExpectedCostOrProceed:  sdk.NewDec(110),
		Account:                "abc",
		OrderType:              types.OrderType_LIMIT,
	})
	assert.Equal(t, *settlements[2], types.Settlement{
		Direction:              types.PositionDirection_LONG,
		PriceSymbol:            TEST_PAIR().PriceDenom,
		AssetSymbol:            TEST_PAIR().AssetDenom,
		Quantity:               sdk.NewDec(8).Quo(sdk.NewDec(3)),
		ExecutionCostOrProceed: sdk.NewDec(100),
		ExpectedCostOrProceed:  sdk.NewDec(100),
		Account:                "abc",
		OrderType:              types.OrderType_LIMIT,
	})
	assert.Equal(t, *settlements[3], types.Settlement{
		Direction:              types.PositionDirection_LONG,
		PriceSymbol:            TEST_PAIR().PriceDenom,
		AssetSymbol:            TEST_PAIR().AssetDenom,
		Quantity:               sdk.NewDec(4).Quo(sdk.NewDec(3)),
		ExecutionCostOrProceed: sdk.NewDec(100),
		ExpectedCostOrProceed:  sdk.NewDec(100),
		Account:                "ghi",
		OrderType:              types.OrderType_LIMIT,
	})
	assert.Equal(t, *settlements[4], types.Settlement{
		Direction:              types.PositionDirection_SHORT,
		PriceSymbol:            TEST_PAIR().PriceDenom,
		AssetSymbol:            TEST_PAIR().AssetDenom,
		Quantity:               sdk.NewDec(1),
		ExecutionCostOrProceed: sdk.MustNewDecFromStr("103.333333333333333333"),
		ExpectedCostOrProceed:  sdk.NewDec(96),
		Account:                "jkl",
		OrderType:              types.OrderType_MARKET,
	})
	assert.Equal(t, *settlements[5], types.Settlement{
		Direction:              types.PositionDirection_SHORT,
		PriceSymbol:            TEST_PAIR().PriceDenom,
		AssetSymbol:            TEST_PAIR().AssetDenom,
		Quantity:               sdk.NewDec(1),
		ExecutionCostOrProceed: sdk.MustNewDecFromStr("103.333333333333333333"),
		ExpectedCostOrProceed:  sdk.NewDec(100),
		Account:                "abc",
		OrderType:              types.OrderType_MARKET,
	})
	assert.Equal(t, *settlements[6], types.Settlement{
		Direction:              types.PositionDirection_SHORT,
		PriceSymbol:            TEST_PAIR().PriceDenom,
		AssetSymbol:            TEST_PAIR().AssetDenom,
		Quantity:               sdk.NewDec(8).Quo(sdk.NewDec(3)),
		ExecutionCostOrProceed: sdk.MustNewDecFromStr("103.333333333333333333"),
		ExpectedCostOrProceed:  sdk.NewDec(100),
		Account:                "abc",
		OrderType:              types.OrderType_MARKET,
	})
	assert.Equal(t, *settlements[7], types.Settlement{
		Direction:              types.PositionDirection_SHORT,
		PriceSymbol:            TEST_PAIR().PriceDenom,
		AssetSymbol:            TEST_PAIR().AssetDenom,
		Quantity:               sdk.NewDec(4).Quo(sdk.NewDec(3)),
		ExecutionCostOrProceed: sdk.MustNewDecFromStr("103.333333333333333333"),
		ExpectedCostOrProceed:  sdk.NewDec(100),
		Account:                "abc",
		OrderType:              types.OrderType_MARKET,
	})
}
