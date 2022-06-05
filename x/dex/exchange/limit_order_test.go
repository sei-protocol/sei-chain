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

func TEST_PAIR() types.Pair {
	return types.Pair{
		PriceDenom: types.Denom_USDC,
		AssetDenom: types.Denom_ATOM,
	}
}

func TestMatchSingleOrder(t *testing.T) {
	_, ctx := keepertest.DexKeeper(t)
	longOrders := []dexcache.LimitOrder{
		{
			Price:     sdk.NewDec(100),
			Quantity:  sdk.NewDec(5),
			Creator:   "abc",
			Direction: types.PositionDirection_LONG,
		},
	}
	shortOrders := []dexcache.LimitOrder{
		{
			Price:     sdk.NewDec(100),
			Quantity:  sdk.NewDec(5),
			Creator:   "def",
			Direction: types.PositionDirection_SHORT,
		},
	}
	longBook := []types.OrderBook{}
	shortBook := []types.OrderBook{}
	dirtyLongPrices := exchange.NewDirtyPrices()
	dirtyShortPrices := exchange.NewDirtyPrices()
	settlements := []*types.Settlement{}
	totalPrice, totalExecuted := exchange.MatchLimitOrders(
		ctx, longOrders, shortOrders, &longBook, &shortBook, TEST_PAIR(), &dirtyLongPrices, &dirtyShortPrices, &settlements,
	)
	assert.Equal(t, totalPrice, sdk.NewDec(1000))
	assert.Equal(t, totalExecuted, sdk.NewDec(10))
	assert.Equal(t, len(dirtyLongPrices.Get()), 1)
	assert.Equal(t, len(dirtyShortPrices.Get()), 1)
	assert.Equal(t, dirtyLongPrices.Has(sdk.NewDec(100)), true)
	assert.Equal(t, dirtyShortPrices.Has(sdk.NewDec(100)), true)
	assert.Equal(t, len(longBook), 1)
	assert.Equal(t, longBook[0].GetPrice(), sdk.NewDec(100))
	assert.Equal(t, longBook[0].GetEntry().Price, sdk.NewDec(100))
	assert.Equal(t, longBook[0].GetEntry().Quantity.IsZero(), true)
	assert.Equal(t, len(shortBook), 1)
	assert.Equal(t, shortBook[0].GetPrice(), sdk.NewDec(100))
	assert.Equal(t, shortBook[0].GetEntry().Price, sdk.NewDec(100))
	assert.Equal(t, shortBook[0].GetEntry().Quantity.IsZero(), true)
	assert.Equal(t, len(settlements), 2)
	assert.Equal(t, *settlements[0], types.Settlement{
		Direction:              types.PositionDirection_LONG,
		PriceSymbol:            TEST_PAIR().PriceDenom,
		AssetSymbol:            TEST_PAIR().AssetDenom,
		Quantity:               sdk.NewDec(5),
		ExecutionCostOrProceed: sdk.NewDec(100),
		ExpectedCostOrProceed:  sdk.NewDec(100),
		Account:                "abc",
	})
	assert.Equal(t, *settlements[1], types.Settlement{
		Direction:              types.PositionDirection_SHORT,
		PriceSymbol:            TEST_PAIR().PriceDenom,
		AssetSymbol:            TEST_PAIR().AssetDenom,
		Quantity:               sdk.NewDec(5),
		ExecutionCostOrProceed: sdk.NewDec(100),
		ExpectedCostOrProceed:  sdk.NewDec(100),
		Account:                "def",
	})
}

func TestAddOrders(t *testing.T) {
	_, ctx := keepertest.DexKeeper(t)
	longOrders := []dexcache.LimitOrder{
		{
			Price:     sdk.NewDec(100),
			Quantity:  sdk.NewDec(5),
			Creator:   "def",
			Direction: types.PositionDirection_LONG,
			Effect:    types.PositionEffect_CLOSE,
		},
		{
			Price:     sdk.NewDec(95),
			Quantity:  sdk.NewDec(3),
			Creator:   "def",
			Direction: types.PositionDirection_LONG,
			Effect:    types.PositionEffect_CLOSE,
		},
	}
	shortOrders := []dexcache.LimitOrder{
		{
			Price:     sdk.NewDec(105),
			Quantity:  sdk.NewDec(10),
			Creator:   "ghi",
			Direction: types.PositionDirection_SHORT,
			Effect:    types.PositionEffect_CLOSE,
			Leverage:  sdk.NewDec(2),
		},
		{
			Price:     sdk.NewDec(115),
			Quantity:  sdk.NewDec(2),
			Creator:   "mno",
			Direction: types.PositionDirection_SHORT,
			Effect:    types.PositionEffect_CLOSE,
		},
	}
	longBook := []types.OrderBook{
		&types.LongBook{
			Price: sdk.NewDec(98),
			Entry: &types.OrderEntry{
				Price:             sdk.NewDec(98),
				Quantity:          sdk.NewDec(5),
				AllocationCreator: []string{"abc|c|<nil>"},
				Allocation:        []sdk.Dec{sdk.NewDec(5)},
				PriceDenom:        TEST_PAIR().PriceDenom,
				AssetDenom:        TEST_PAIR().AssetDenom,
			},
		},
		&types.LongBook{
			Price: sdk.NewDec(100),
			Entry: &types.OrderEntry{
				Price:             sdk.NewDec(100),
				Quantity:          sdk.NewDec(3),
				AllocationCreator: []string{"def|c|<nil>"},
				Allocation:        []sdk.Dec{sdk.NewDec(3)},
				PriceDenom:        TEST_PAIR().PriceDenom,
				AssetDenom:        TEST_PAIR().AssetDenom,
			},
		},
	}
	shortBook := []types.OrderBook{
		&types.ShortBook{
			Price: sdk.NewDec(101),
			Entry: &types.OrderEntry{
				Price:             sdk.NewDec(101),
				Quantity:          sdk.NewDec(5),
				AllocationCreator: []string{"abc|c|<nil>"},
				Allocation:        []sdk.Dec{sdk.NewDec(5)},
				PriceDenom:        TEST_PAIR().PriceDenom,
				AssetDenom:        TEST_PAIR().AssetDenom,
			},
		},
		&types.ShortBook{
			Price: sdk.NewDec(115),
			Entry: &types.OrderEntry{
				Price:             sdk.NewDec(115),
				Quantity:          sdk.NewDec(3),
				AllocationCreator: []string{"def|c|<nil>"},
				Allocation:        []sdk.Dec{sdk.NewDec(3)},
				PriceDenom:        TEST_PAIR().PriceDenom,
				AssetDenom:        TEST_PAIR().AssetDenom,
			},
		},
	}
	dirtyLongPrices := exchange.NewDirtyPrices()
	dirtyShortPrices := exchange.NewDirtyPrices()
	settlements := []*types.Settlement{}
	totalPrice, totalExecuted := exchange.MatchLimitOrders(
		ctx, longOrders, shortOrders, &longBook, &shortBook, TEST_PAIR(), &dirtyLongPrices, &dirtyShortPrices, &settlements,
	)
	assert.Equal(t, totalPrice, sdk.NewDec(0))
	assert.Equal(t, totalExecuted, sdk.NewDec(0))
	assert.Equal(t, len(dirtyLongPrices.Get()), 2)
	assert.Equal(t, len(dirtyShortPrices.Get()), 2)
	assert.Equal(t, dirtyLongPrices.Has(sdk.NewDec(95)), true)
	assert.Equal(t, dirtyLongPrices.Has(sdk.NewDec(100)), true)
	assert.Equal(t, dirtyShortPrices.Has(sdk.NewDec(105)), true)
	assert.Equal(t, dirtyShortPrices.Has(sdk.NewDec(115)), true)
	assert.Equal(t, len(longBook), 3)
	assert.Equal(t, longBook[0].GetPrice(), sdk.NewDec(95))
	assert.Equal(t, *longBook[0].GetEntry(), types.OrderEntry{
		Price:             sdk.NewDec(95),
		Quantity:          sdk.NewDec(3),
		AllocationCreator: []string{"def|c|<nil>"},
		Allocation:        []sdk.Dec{sdk.NewDec(3)},
		PriceDenom:        TEST_PAIR().PriceDenom,
		AssetDenom:        TEST_PAIR().AssetDenom,
	})
	assert.Equal(t, longBook[1].GetPrice(), sdk.NewDec(98))
	assert.Equal(t, *longBook[1].GetEntry(), types.OrderEntry{
		Price:             sdk.NewDec(98),
		Quantity:          sdk.NewDec(5),
		AllocationCreator: []string{"abc|c|<nil>"},
		Allocation:        []sdk.Dec{sdk.NewDec(5)},
		PriceDenom:        TEST_PAIR().PriceDenom,
		AssetDenom:        TEST_PAIR().AssetDenom,
	})
	assert.Equal(t, longBook[2].GetPrice(), sdk.NewDec(100))
	assert.Equal(t, *longBook[2].GetEntry(), types.OrderEntry{
		Price:             sdk.NewDec(100),
		Quantity:          sdk.NewDec(8),
		AllocationCreator: []string{"def|c|<nil>"},
		Allocation:        []sdk.Dec{sdk.NewDec(8)},
		PriceDenom:        TEST_PAIR().PriceDenom,
		AssetDenom:        TEST_PAIR().AssetDenom,
	})
	assert.Equal(t, len(shortBook), 3)
	assert.Equal(t, shortBook[0].GetPrice(), sdk.NewDec(101))
	assert.Equal(t, *shortBook[0].GetEntry(), types.OrderEntry{
		Price:             sdk.NewDec(101),
		Quantity:          sdk.NewDec(5),
		AllocationCreator: []string{"abc|c|<nil>"},
		Allocation:        []sdk.Dec{sdk.NewDec(5)},
		PriceDenom:        TEST_PAIR().PriceDenom,
		AssetDenom:        TEST_PAIR().AssetDenom,
	})
	assert.Equal(t, shortBook[1].GetPrice(), sdk.NewDec(105))
	assert.Equal(t, *shortBook[1].GetEntry(), types.OrderEntry{
		Price:             sdk.NewDec(105),
		Quantity:          sdk.NewDec(10),
		AllocationCreator: []string{"ghi|c|2.000000000000000000"},
		Allocation:        []sdk.Dec{sdk.NewDec(10)},
		PriceDenom:        TEST_PAIR().PriceDenom,
		AssetDenom:        TEST_PAIR().AssetDenom,
	})
	assert.Equal(t, shortBook[2].GetPrice(), sdk.NewDec(115))
	assert.Equal(t, *shortBook[2].GetEntry(), types.OrderEntry{
		Price:             sdk.NewDec(115),
		Quantity:          sdk.NewDec(5),
		AllocationCreator: []string{"def|c|<nil>", "mno|c|<nil>"},
		Allocation:        []sdk.Dec{sdk.NewDec(3), sdk.NewDec(2)},
		PriceDenom:        TEST_PAIR().PriceDenom,
		AssetDenom:        TEST_PAIR().AssetDenom,
	})
	assert.Equal(t, len(settlements), 0)
}

func TestMatchSingleOrderFromShortBook(t *testing.T) {
	_, ctx := keepertest.DexKeeper(t)
	longOrders := []dexcache.LimitOrder{
		{
			Price:     sdk.NewDec(100),
			Quantity:  sdk.NewDec(5),
			Creator:   "abc",
			Direction: types.PositionDirection_LONG,
		},
	}
	shortOrders := []dexcache.LimitOrder{}
	longBook := []types.OrderBook{}
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
	dirtyLongPrices := exchange.NewDirtyPrices()
	dirtyShortPrices := exchange.NewDirtyPrices()
	settlements := []*types.Settlement{}
	totalPrice, totalExecuted := exchange.MatchLimitOrders(
		ctx, longOrders, shortOrders, &longBook, &shortBook, TEST_PAIR(), &dirtyLongPrices, &dirtyShortPrices, &settlements,
	)
	assert.Equal(t, totalPrice, sdk.NewDec(1000))
	assert.Equal(t, totalExecuted, sdk.NewDec(10))
	assert.Equal(t, len(dirtyLongPrices.Get()), 1)
	assert.Equal(t, len(dirtyShortPrices.Get()), 1)
	assert.Equal(t, dirtyLongPrices.Has(sdk.NewDec(100)), true)
	assert.Equal(t, dirtyShortPrices.Has(sdk.NewDec(100)), true)
	assert.Equal(t, len(longBook), 1)
	assert.Equal(t, longBook[0].GetPrice(), sdk.NewDec(100))
	assert.Equal(t, longBook[0].GetEntry().Price, sdk.NewDec(100))
	assert.Equal(t, longBook[0].GetEntry().Quantity.IsZero(), true)
	assert.Equal(t, len(shortBook), 1)
	assert.Equal(t, shortBook[0].GetPrice(), sdk.NewDec(100))
	assert.Equal(t, shortBook[0].GetEntry().Price, sdk.NewDec(100))
	assert.Equal(t, shortBook[0].GetEntry().Quantity.IsZero(), true)
	assert.Equal(t, len(settlements), 2)
	assert.Equal(t, *settlements[0], types.Settlement{
		Direction:              types.PositionDirection_LONG,
		PriceSymbol:            TEST_PAIR().PriceDenom,
		AssetSymbol:            TEST_PAIR().AssetDenom,
		Quantity:               sdk.NewDec(5),
		ExecutionCostOrProceed: sdk.NewDec(100),
		ExpectedCostOrProceed:  sdk.NewDec(100),
		Account:                "abc",
	})
	assert.Equal(t, *settlements[1], types.Settlement{
		Direction:              types.PositionDirection_SHORT,
		PriceSymbol:            TEST_PAIR().PriceDenom,
		AssetSymbol:            TEST_PAIR().AssetDenom,
		Quantity:               sdk.NewDec(5),
		ExecutionCostOrProceed: sdk.NewDec(100),
		ExpectedCostOrProceed:  sdk.NewDec(100),
		Account:                "def",
	})
}

func TestMatchSingleOrderFromLongBook(t *testing.T) {
	_, ctx := keepertest.DexKeeper(t)
	longOrders := []dexcache.LimitOrder{}
	shortOrders := []dexcache.LimitOrder{
		{
			Price:     sdk.NewDec(100),
			Quantity:  sdk.NewDec(5),
			Creator:   "def",
			Direction: types.PositionDirection_SHORT,
		},
	}
	longBook := []types.OrderBook{
		&types.LongBook{
			Price: sdk.NewDec(100),
			Entry: &types.OrderEntry{
				Price:             sdk.NewDec(100),
				Quantity:          sdk.NewDec(5),
				AllocationCreator: []string{"abc|c|"},
				Allocation:        []sdk.Dec{sdk.NewDec(5)},
				PriceDenom:        TEST_PAIR().PriceDenom,
				AssetDenom:        TEST_PAIR().AssetDenom,
			},
		},
	}
	shortBook := []types.OrderBook{}
	dirtyLongPrices := exchange.NewDirtyPrices()
	dirtyShortPrices := exchange.NewDirtyPrices()
	settlements := []*types.Settlement{}
	totalPrice, totalExecuted := exchange.MatchLimitOrders(
		ctx, longOrders, shortOrders, &longBook, &shortBook, TEST_PAIR(), &dirtyLongPrices, &dirtyShortPrices, &settlements,
	)
	assert.Equal(t, totalPrice, sdk.NewDec(1000))
	assert.Equal(t, totalExecuted, sdk.NewDec(10))
	assert.Equal(t, len(dirtyLongPrices.Get()), 1)
	assert.Equal(t, len(dirtyShortPrices.Get()), 1)
	assert.Equal(t, dirtyLongPrices.Has(sdk.NewDec(100)), true)
	assert.Equal(t, dirtyShortPrices.Has(sdk.NewDec(100)), true)
	assert.Equal(t, len(longBook), 1)
	assert.Equal(t, longBook[0].GetPrice(), sdk.NewDec(100))
	assert.Equal(t, longBook[0].GetEntry().Price, sdk.NewDec(100))
	assert.Equal(t, longBook[0].GetEntry().Quantity.IsZero(), true)
	assert.Equal(t, len(shortBook), 1)
	assert.Equal(t, shortBook[0].GetPrice(), sdk.NewDec(100))
	assert.Equal(t, shortBook[0].GetEntry().Price, sdk.NewDec(100))
	assert.Equal(t, shortBook[0].GetEntry().Quantity.IsZero(), true)
	assert.Equal(t, len(settlements), 2)
	assert.Equal(t, *settlements[0], types.Settlement{
		Direction:              types.PositionDirection_LONG,
		PriceSymbol:            TEST_PAIR().PriceDenom,
		AssetSymbol:            TEST_PAIR().AssetDenom,
		Quantity:               sdk.NewDec(5),
		ExecutionCostOrProceed: sdk.NewDec(100),
		ExpectedCostOrProceed:  sdk.NewDec(100),
		Account:                "abc",
	})
	assert.Equal(t, *settlements[1], types.Settlement{
		Direction:              types.PositionDirection_SHORT,
		PriceSymbol:            TEST_PAIR().PriceDenom,
		AssetSymbol:            TEST_PAIR().AssetDenom,
		Quantity:               sdk.NewDec(5),
		ExecutionCostOrProceed: sdk.NewDec(100),
		ExpectedCostOrProceed:  sdk.NewDec(100),
		Account:                "def",
	})
}

func TestMatchSingleOrderFromMultipleShortBook(t *testing.T) {
	_, ctx := keepertest.DexKeeper(t)
	longOrders := []dexcache.LimitOrder{
		{
			Price:     sdk.NewDec(100),
			Quantity:  sdk.NewDec(5),
			Creator:   "abc",
			Direction: types.PositionDirection_LONG,
		},
	}
	shortOrders := []dexcache.LimitOrder{}
	longBook := []types.OrderBook{}
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
	dirtyLongPrices := exchange.NewDirtyPrices()
	dirtyShortPrices := exchange.NewDirtyPrices()
	settlements := []*types.Settlement{}
	totalPrice, totalExecuted := exchange.MatchLimitOrders(
		ctx, longOrders, shortOrders, &longBook, &shortBook, TEST_PAIR(), &dirtyLongPrices, &dirtyShortPrices, &settlements,
	)
	assert.Equal(t, totalPrice, sdk.NewDec(980))
	assert.Equal(t, totalExecuted, sdk.NewDec(10))
	assert.Equal(t, len(dirtyLongPrices.Get()), 1)
	assert.Equal(t, len(dirtyShortPrices.Get()), 2)
	assert.Equal(t, dirtyLongPrices.Has(sdk.NewDec(100)), true)
	assert.Equal(t, dirtyShortPrices.Has(sdk.NewDec(90)), true)
	assert.Equal(t, dirtyShortPrices.Has(sdk.NewDec(100)), true)
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
		Price:             sdk.NewDec(100),
		Quantity:          sdk.NewDec(3),
		AllocationCreator: []string{"def|c|", "ghi|c|"},
		Allocation:        []sdk.Dec{sdk.NewDec(2), sdk.NewDec(1)},
		PriceDenom:        TEST_PAIR().PriceDenom,
		AssetDenom:        TEST_PAIR().AssetDenom,
	})
	assert.Equal(t, len(settlements), 6)
	assert.Equal(t, *settlements[0], types.Settlement{
		Direction:              types.PositionDirection_LONG,
		PriceSymbol:            TEST_PAIR().PriceDenom,
		AssetSymbol:            TEST_PAIR().AssetDenom,
		Quantity:               sdk.NewDec(2),
		ExecutionCostOrProceed: sdk.NewDec(95),
		ExpectedCostOrProceed:  sdk.NewDec(100),
		Account:                "abc",
	})
	assert.Equal(t, *settlements[1], types.Settlement{
		Direction:              types.PositionDirection_SHORT,
		PriceSymbol:            TEST_PAIR().PriceDenom,
		AssetSymbol:            TEST_PAIR().AssetDenom,
		Quantity:               sdk.NewDec(2),
		ExecutionCostOrProceed: sdk.NewDec(95),
		ExpectedCostOrProceed:  sdk.NewDec(90),
		Account:                "def",
	})
	assert.Equal(t, *settlements[2], types.Settlement{
		Direction:              types.PositionDirection_LONG,
		PriceSymbol:            TEST_PAIR().PriceDenom,
		AssetSymbol:            TEST_PAIR().AssetDenom,
		Quantity:               sdk.NewDec(2),
		ExecutionCostOrProceed: sdk.NewDec(100),
		ExpectedCostOrProceed:  sdk.NewDec(100),
		Account:                "abc",
	})
	assert.Equal(t, *settlements[3], types.Settlement{
		Direction:              types.PositionDirection_SHORT,
		PriceSymbol:            TEST_PAIR().PriceDenom,
		AssetSymbol:            TEST_PAIR().AssetDenom,
		Quantity:               sdk.NewDec(2),
		ExecutionCostOrProceed: sdk.NewDec(100),
		ExpectedCostOrProceed:  sdk.NewDec(100),
		Account:                "def",
	})
	assert.Equal(t, *settlements[4], types.Settlement{
		Direction:              types.PositionDirection_LONG,
		PriceSymbol:            TEST_PAIR().PriceDenom,
		AssetSymbol:            TEST_PAIR().AssetDenom,
		Quantity:               sdk.NewDec(1),
		ExecutionCostOrProceed: sdk.NewDec(100),
		ExpectedCostOrProceed:  sdk.NewDec(100),
		Account:                "abc",
	})
	assert.Equal(t, *settlements[5], types.Settlement{
		Direction:              types.PositionDirection_SHORT,
		PriceSymbol:            TEST_PAIR().PriceDenom,
		AssetSymbol:            TEST_PAIR().AssetDenom,
		Quantity:               sdk.NewDec(1),
		ExecutionCostOrProceed: sdk.NewDec(100),
		ExpectedCostOrProceed:  sdk.NewDec(100),
		Account:                "ghi",
	})
}

func TestMatchSingleOrderFromMultipleLongBook(t *testing.T) {
	_, ctx := keepertest.DexKeeper(t)
	longOrders := []dexcache.LimitOrder{}
	shortOrders := []dexcache.LimitOrder{
		{
			Price:     sdk.NewDec(100),
			Quantity:  sdk.NewDec(5),
			Creator:   "def",
			Direction: types.PositionDirection_SHORT,
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
	shortBook := []types.OrderBook{}
	dirtyLongPrices := exchange.NewDirtyPrices()
	dirtyShortPrices := exchange.NewDirtyPrices()
	settlements := []*types.Settlement{}
	totalPrice, totalExecuted := exchange.MatchLimitOrders(
		ctx, longOrders, shortOrders, &longBook, &shortBook, TEST_PAIR(), &dirtyLongPrices, &dirtyShortPrices, &settlements,
	)
	assert.Equal(t, totalPrice, sdk.NewDec(1020))
	assert.Equal(t, totalExecuted, sdk.NewDec(10))
	assert.Equal(t, len(dirtyLongPrices.Get()), 2)
	assert.Equal(t, len(dirtyShortPrices.Get()), 1)
	assert.Equal(t, dirtyLongPrices.Has(sdk.NewDec(100)), true)
	assert.Equal(t, dirtyLongPrices.Has(sdk.NewDec(110)), true)
	assert.Equal(t, dirtyShortPrices.Has(sdk.NewDec(100)), true)
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
	assert.Equal(t, len(shortBook), 1)
	assert.Equal(t, shortBook[0].GetPrice(), sdk.NewDec(100))
	assert.Equal(t, shortBook[0].GetEntry().Price, sdk.NewDec(100))
	assert.Equal(t, shortBook[0].GetEntry().Quantity.IsZero(), true)
	assert.Equal(t, len(settlements), 6)
	assert.Equal(t, *settlements[0], types.Settlement{
		Direction:              types.PositionDirection_LONG,
		PriceSymbol:            TEST_PAIR().PriceDenom,
		AssetSymbol:            TEST_PAIR().AssetDenom,
		Quantity:               sdk.NewDec(2),
		ExecutionCostOrProceed: sdk.NewDec(105),
		ExpectedCostOrProceed:  sdk.NewDec(110),
		Account:                "abc",
	})
	assert.Equal(t, *settlements[1], types.Settlement{
		Direction:              types.PositionDirection_SHORT,
		PriceSymbol:            TEST_PAIR().PriceDenom,
		AssetSymbol:            TEST_PAIR().AssetDenom,
		Quantity:               sdk.NewDec(2),
		ExecutionCostOrProceed: sdk.NewDec(105),
		ExpectedCostOrProceed:  sdk.NewDec(100),
		Account:                "def",
	})
	assert.Equal(t, *settlements[2], types.Settlement{
		Direction:              types.PositionDirection_LONG,
		PriceSymbol:            TEST_PAIR().PriceDenom,
		AssetSymbol:            TEST_PAIR().AssetDenom,
		Quantity:               sdk.NewDec(2),
		ExecutionCostOrProceed: sdk.NewDec(100),
		ExpectedCostOrProceed:  sdk.NewDec(100),
		Account:                "abc",
	})
	assert.Equal(t, *settlements[3], types.Settlement{
		Direction:              types.PositionDirection_SHORT,
		PriceSymbol:            TEST_PAIR().PriceDenom,
		AssetSymbol:            TEST_PAIR().AssetDenom,
		Quantity:               sdk.NewDec(2),
		ExecutionCostOrProceed: sdk.NewDec(100),
		ExpectedCostOrProceed:  sdk.NewDec(100),
		Account:                "def",
	})
	assert.Equal(t, *settlements[4], types.Settlement{
		Direction:              types.PositionDirection_LONG,
		PriceSymbol:            TEST_PAIR().PriceDenom,
		AssetSymbol:            TEST_PAIR().AssetDenom,
		Quantity:               sdk.NewDec(1),
		ExecutionCostOrProceed: sdk.NewDec(100),
		ExpectedCostOrProceed:  sdk.NewDec(100),
		Account:                "ghi",
	})
	assert.Equal(t, *settlements[5], types.Settlement{
		Direction:              types.PositionDirection_SHORT,
		PriceSymbol:            TEST_PAIR().PriceDenom,
		AssetSymbol:            TEST_PAIR().AssetDenom,
		Quantity:               sdk.NewDec(1),
		ExecutionCostOrProceed: sdk.NewDec(100),
		ExpectedCostOrProceed:  sdk.NewDec(100),
		Account:                "def",
	})
}

func TestMatchMultipleOrderFromMultipleShortBook(t *testing.T) {
	_, ctx := keepertest.DexKeeper(t)
	longOrders := []dexcache.LimitOrder{
		{
			Price:     sdk.NewDec(100),
			Quantity:  sdk.NewDec(5),
			Creator:   "abc",
			Direction: types.PositionDirection_LONG,
			Effect:    types.PositionEffect_CLOSE,
		},
		{
			Price:     sdk.NewDec(104),
			Quantity:  sdk.NewDec(1),
			Creator:   "jkl",
			Direction: types.PositionDirection_LONG,
			Effect:    types.PositionEffect_CLOSE,
		},
		{
			Price:     sdk.NewDec(98),
			Quantity:  sdk.NewDec(2),
			Creator:   "mno",
			Direction: types.PositionDirection_LONG,
			Effect:    types.PositionEffect_CLOSE,
		},
	}
	shortOrders := []dexcache.LimitOrder{}
	longBook := []types.OrderBook{}
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
	dirtyLongPrices := exchange.NewDirtyPrices()
	dirtyShortPrices := exchange.NewDirtyPrices()
	settlements := []*types.Settlement{}
	totalPrice, totalExecuted := exchange.MatchLimitOrders(
		ctx, longOrders, shortOrders, &longBook, &shortBook, TEST_PAIR(), &dirtyLongPrices, &dirtyShortPrices, &settlements,
	)
	assert.Equal(t, totalPrice, sdk.NewDec(1184))
	assert.Equal(t, totalExecuted, sdk.NewDec(12))
	assert.Equal(t, len(dirtyLongPrices.Get()), 3)
	assert.Equal(t, len(dirtyShortPrices.Get()), 2)
	assert.Equal(t, dirtyLongPrices.Has(sdk.NewDec(98)), true)
	assert.Equal(t, dirtyLongPrices.Has(sdk.NewDec(100)), true)
	assert.Equal(t, dirtyLongPrices.Has(sdk.NewDec(104)), true)
	assert.Equal(t, dirtyShortPrices.Has(sdk.NewDec(90)), true)
	assert.Equal(t, dirtyShortPrices.Has(sdk.NewDec(100)), true)
	assert.Equal(t, len(longBook), 3)
	assert.Equal(t, longBook[0].GetPrice(), sdk.NewDec(98))
	assert.Equal(t, *longBook[0].GetEntry(), types.OrderEntry{
		Price:             sdk.NewDec(98),
		Quantity:          sdk.NewDec(2),
		AllocationCreator: []string{"mno|c|<nil>"},
		Allocation:        []sdk.Dec{sdk.NewDec(2)},
		PriceDenom:        TEST_PAIR().PriceDenom,
		AssetDenom:        TEST_PAIR().AssetDenom,
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
		Price:             sdk.NewDec(100),
		Quantity:          sdk.NewDec(2),
		AllocationCreator: []string{"def|c|", "ghi|c|"},
		Allocation:        []sdk.Dec{sdk.NewDec(4).Quo(sdk.NewDec(3)), sdk.NewDec(2).Quo(sdk.NewDec(3))},
		PriceDenom:        TEST_PAIR().PriceDenom,
		AssetDenom:        TEST_PAIR().AssetDenom,
	})
	assert.Equal(t, len(settlements), 8)
	assert.Equal(t, *settlements[0], types.Settlement{
		Direction:              types.PositionDirection_LONG,
		PriceSymbol:            TEST_PAIR().PriceDenom,
		AssetSymbol:            TEST_PAIR().AssetDenom,
		Quantity:               sdk.NewDec(1),
		ExecutionCostOrProceed: sdk.NewDec(97),
		ExpectedCostOrProceed:  sdk.NewDec(104),
		Account:                "jkl",
	})
	assert.Equal(t, *settlements[1], types.Settlement{
		Direction:              types.PositionDirection_SHORT,
		PriceSymbol:            TEST_PAIR().PriceDenom,
		AssetSymbol:            TEST_PAIR().AssetDenom,
		Quantity:               sdk.NewDec(1),
		ExecutionCostOrProceed: sdk.NewDec(97),
		ExpectedCostOrProceed:  sdk.NewDec(90),
		Account:                "def",
	})
	assert.Equal(t, *settlements[2], types.Settlement{
		Direction:              types.PositionDirection_LONG,
		PriceSymbol:            TEST_PAIR().PriceDenom,
		AssetSymbol:            TEST_PAIR().AssetDenom,
		Quantity:               sdk.NewDec(1),
		ExecutionCostOrProceed: sdk.NewDec(95),
		ExpectedCostOrProceed:  sdk.NewDec(100),
		Account:                "abc",
	})
	assert.Equal(t, *settlements[3], types.Settlement{
		Direction:              types.PositionDirection_SHORT,
		PriceSymbol:            TEST_PAIR().PriceDenom,
		AssetSymbol:            TEST_PAIR().AssetDenom,
		Quantity:               sdk.NewDec(1),
		ExecutionCostOrProceed: sdk.NewDec(95),
		ExpectedCostOrProceed:  sdk.NewDec(90),
		Account:                "def",
	})
	assert.Equal(t, *settlements[4], types.Settlement{
		Direction:              types.PositionDirection_LONG,
		PriceSymbol:            TEST_PAIR().PriceDenom,
		AssetSymbol:            TEST_PAIR().AssetDenom,
		Quantity:               sdk.NewDec(8).Quo(sdk.NewDec(3)),
		ExecutionCostOrProceed: sdk.NewDec(100),
		ExpectedCostOrProceed:  sdk.NewDec(100),
		Account:                "abc",
	})
	assert.Equal(t, *settlements[5], types.Settlement{
		Direction:              types.PositionDirection_SHORT,
		PriceSymbol:            TEST_PAIR().PriceDenom,
		AssetSymbol:            TEST_PAIR().AssetDenom,
		Quantity:               sdk.NewDec(8).Quo(sdk.NewDec(3)),
		ExecutionCostOrProceed: sdk.NewDec(100),
		ExpectedCostOrProceed:  sdk.NewDec(100),
		Account:                "def",
	})
	assert.Equal(t, *settlements[6], types.Settlement{
		Direction:              types.PositionDirection_LONG,
		PriceSymbol:            TEST_PAIR().PriceDenom,
		AssetSymbol:            TEST_PAIR().AssetDenom,
		Quantity:               sdk.NewDec(4).Quo(sdk.NewDec(3)),
		ExecutionCostOrProceed: sdk.NewDec(100),
		ExpectedCostOrProceed:  sdk.NewDec(100),
		Account:                "abc",
	})
	assert.Equal(t, *settlements[7], types.Settlement{
		Direction:              types.PositionDirection_SHORT,
		PriceSymbol:            TEST_PAIR().PriceDenom,
		AssetSymbol:            TEST_PAIR().AssetDenom,
		Quantity:               sdk.NewDec(4).Quo(sdk.NewDec(3)),
		ExecutionCostOrProceed: sdk.NewDec(100),
		ExpectedCostOrProceed:  sdk.NewDec(100),
		Account:                "ghi",
	})
}

func TestMatchMultipleOrderFromMultipleLongBook(t *testing.T) {
	_, ctx := keepertest.DexKeeper(t)
	longOrders := []dexcache.LimitOrder{}
	shortOrders := []dexcache.LimitOrder{
		{
			Price:     sdk.NewDec(100),
			Quantity:  sdk.NewDec(5),
			Creator:   "abc",
			Direction: types.PositionDirection_SHORT,
			Effect:    types.PositionEffect_CLOSE,
		},
		{
			Price:     sdk.NewDec(96),
			Quantity:  sdk.NewDec(1),
			Creator:   "jkl",
			Direction: types.PositionDirection_SHORT,
			Effect:    types.PositionEffect_CLOSE,
		},
		{
			Price:     sdk.NewDec(102),
			Quantity:  sdk.NewDec(2),
			Creator:   "mno",
			Direction: types.PositionDirection_SHORT,
			Effect:    types.PositionEffect_CLOSE,
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
	shortBook := []types.OrderBook{}
	dirtyLongPrices := exchange.NewDirtyPrices()
	dirtyShortPrices := exchange.NewDirtyPrices()
	settlements := []*types.Settlement{}
	totalPrice, totalExecuted := exchange.MatchLimitOrders(
		ctx, longOrders, shortOrders, &longBook, &shortBook, TEST_PAIR(), &dirtyLongPrices, &dirtyShortPrices, &settlements,
	)
	assert.Equal(t, totalPrice, sdk.NewDec(1216))
	assert.Equal(t, totalExecuted, sdk.NewDec(12))
	assert.Equal(t, len(dirtyLongPrices.Get()), 2)
	assert.Equal(t, len(dirtyShortPrices.Get()), 3)
	assert.Equal(t, dirtyLongPrices.Has(sdk.NewDec(100)), true)
	assert.Equal(t, dirtyLongPrices.Has(sdk.NewDec(110)), true)
	assert.Equal(t, dirtyShortPrices.Has(sdk.NewDec(96)), true)
	assert.Equal(t, dirtyShortPrices.Has(sdk.NewDec(100)), true)
	assert.Equal(t, dirtyShortPrices.Has(sdk.NewDec(102)), true)
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
	assert.Equal(t, len(shortBook), 3)
	assert.Equal(t, shortBook[0].GetPrice(), sdk.NewDec(96))
	assert.Equal(t, shortBook[0].GetEntry().Price, sdk.NewDec(96))
	assert.Equal(t, shortBook[0].GetEntry().Quantity.IsZero(), true)
	assert.Equal(t, shortBook[1].GetPrice(), sdk.NewDec(100))
	assert.Equal(t, shortBook[1].GetEntry().Price, sdk.NewDec(100))
	assert.Equal(t, shortBook[1].GetEntry().Quantity.IsZero(), true)
	assert.Equal(t, shortBook[2].GetPrice(), sdk.NewDec(102))
	assert.Equal(t, *shortBook[2].GetEntry(), types.OrderEntry{
		Price:             sdk.NewDec(102),
		Quantity:          sdk.NewDec(2),
		AllocationCreator: []string{"mno|c|<nil>"},
		Allocation:        []sdk.Dec{sdk.NewDec(2)},
		PriceDenom:        TEST_PAIR().PriceDenom,
		AssetDenom:        TEST_PAIR().AssetDenom,
	})
	assert.Equal(t, len(settlements), 8)
	assert.Equal(t, *settlements[0], types.Settlement{
		Direction:              types.PositionDirection_LONG,
		PriceSymbol:            TEST_PAIR().PriceDenom,
		AssetSymbol:            TEST_PAIR().AssetDenom,
		Quantity:               sdk.NewDec(1),
		ExecutionCostOrProceed: sdk.NewDec(103),
		ExpectedCostOrProceed:  sdk.NewDec(110),
		Account:                "abc",
	})
	assert.Equal(t, *settlements[1], types.Settlement{
		Direction:              types.PositionDirection_SHORT,
		PriceSymbol:            TEST_PAIR().PriceDenom,
		AssetSymbol:            TEST_PAIR().AssetDenom,
		Quantity:               sdk.NewDec(1),
		ExecutionCostOrProceed: sdk.NewDec(103),
		ExpectedCostOrProceed:  sdk.NewDec(96),
		Account:                "jkl",
	})
	assert.Equal(t, *settlements[2], types.Settlement{
		Direction:              types.PositionDirection_LONG,
		PriceSymbol:            TEST_PAIR().PriceDenom,
		AssetSymbol:            TEST_PAIR().AssetDenom,
		Quantity:               sdk.NewDec(1),
		ExecutionCostOrProceed: sdk.NewDec(105),
		ExpectedCostOrProceed:  sdk.NewDec(110),
		Account:                "abc",
	})
	assert.Equal(t, *settlements[3], types.Settlement{
		Direction:              types.PositionDirection_SHORT,
		PriceSymbol:            TEST_PAIR().PriceDenom,
		AssetSymbol:            TEST_PAIR().AssetDenom,
		Quantity:               sdk.NewDec(1),
		ExecutionCostOrProceed: sdk.NewDec(105),
		ExpectedCostOrProceed:  sdk.NewDec(100),
		Account:                "abc",
	})
	assert.Equal(t, *settlements[4], types.Settlement{
		Direction:              types.PositionDirection_LONG,
		PriceSymbol:            TEST_PAIR().PriceDenom,
		AssetSymbol:            TEST_PAIR().AssetDenom,
		Quantity:               sdk.NewDec(8).Quo(sdk.NewDec(3)),
		ExecutionCostOrProceed: sdk.NewDec(100),
		ExpectedCostOrProceed:  sdk.NewDec(100),
		Account:                "abc",
	})
	assert.Equal(t, *settlements[5], types.Settlement{
		Direction:              types.PositionDirection_SHORT,
		PriceSymbol:            TEST_PAIR().PriceDenom,
		AssetSymbol:            TEST_PAIR().AssetDenom,
		Quantity:               sdk.NewDec(8).Quo(sdk.NewDec(3)),
		ExecutionCostOrProceed: sdk.NewDec(100),
		ExpectedCostOrProceed:  sdk.NewDec(100),
		Account:                "abc",
	})
	assert.Equal(t, *settlements[6], types.Settlement{
		Direction:              types.PositionDirection_LONG,
		PriceSymbol:            TEST_PAIR().PriceDenom,
		AssetSymbol:            TEST_PAIR().AssetDenom,
		Quantity:               sdk.NewDec(4).Quo(sdk.NewDec(3)),
		ExecutionCostOrProceed: sdk.NewDec(100),
		ExpectedCostOrProceed:  sdk.NewDec(100),
		Account:                "ghi",
	})
	assert.Equal(t, *settlements[7], types.Settlement{
		Direction:              types.PositionDirection_SHORT,
		PriceSymbol:            TEST_PAIR().PriceDenom,
		AssetSymbol:            TEST_PAIR().AssetDenom,
		Quantity:               sdk.NewDec(4).Quo(sdk.NewDec(3)),
		ExecutionCostOrProceed: sdk.NewDec(100),
		ExpectedCostOrProceed:  sdk.NewDec(100),
		Account:                "abc",
	})
}
