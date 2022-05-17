package exchange_test

import (
	"testing"

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
			WorstPrice: 100,
			Quantity:   5,
			Creator:    "abc",
			Long:       true,
		},
	}
	shortBook := []types.OrderBook{
		&types.ShortBook{
			Id: 100,
			Entry: &types.OrderEntry{
				Price:             100,
				Quantity:          5,
				AllocationCreator: []string{"def|c|"},
				Allocation:        []uint64{5},
				PriceDenom:        TEST_PAIR().PriceDenom,
				AssetDenom:        TEST_PAIR().AssetDenom,
			},
		},
	}
	dirtyIds := map[uint64]bool{}
	settlements := []*types.Settlement{}
	totalPrice, totalExecuted := exchange.MatchMarketOrders(
		ctx, longOrders, shortBook, TEST_PAIR(), true, dirtyIds, &settlements,
	)
	assert.Equal(t, totalPrice, uint64(500))
	assert.Equal(t, totalExecuted, uint64(5))
	assert.Equal(t, len(dirtyIds), 1)
	assert.Equal(t, dirtyIds[uint64(100)], true)
	assert.Equal(t, len(shortBook), 1)
	assert.Equal(t, shortBook[0].GetId(), uint64(100))
	assert.Equal(t, *shortBook[0].GetEntry(), types.OrderEntry{
		Price:             uint64(100),
		Quantity:          uint64(0),
		AllocationCreator: []string{},
		Allocation:        []uint64{},
		PriceDenom:        TEST_PAIR().PriceDenom,
		AssetDenom:        TEST_PAIR().AssetDenom,
	})
	assert.Equal(t, len(settlements), 2)
	assert.Equal(t, *settlements[0], types.Settlement{
		Long:                   true,
		PriceSymbol:            TEST_PAIR().PriceDenom,
		AssetSymbol:            TEST_PAIR().AssetDenom,
		Quantity:               uint64(5),
		ExecutionCostOrProceed: uint64(100),
		ExpectedCostOrProceed:  uint64(100),
		Account:                "abc",
	})
	assert.Equal(t, *settlements[1], types.Settlement{
		Long:                   false,
		PriceSymbol:            TEST_PAIR().PriceDenom,
		AssetSymbol:            TEST_PAIR().AssetDenom,
		Quantity:               uint64(5),
		ExecutionCostOrProceed: uint64(100),
		ExpectedCostOrProceed:  uint64(100),
		Account:                "def",
	})
}

func TestMatchSingleMarketOrderFromLongBook(t *testing.T) {
	_, ctx := keepertest.DexKeeper(t)
	shortOrders := []dexcache.MarketOrder{
		{
			WorstPrice: 100,
			Quantity:   5,
			Creator:    "abc",
			Long:       false,
		},
	}
	longBook := []types.OrderBook{
		&types.LongBook{
			Id: 100,
			Entry: &types.OrderEntry{
				Price:             100,
				Quantity:          5,
				AllocationCreator: []string{"def|c|"},
				Allocation:        []uint64{5},
				PriceDenom:        TEST_PAIR().PriceDenom,
				AssetDenom:        TEST_PAIR().AssetDenom,
			},
		},
	}
	dirtyIds := map[uint64]bool{}
	settlements := []*types.Settlement{}
	totalPrice, totalExecuted := exchange.MatchMarketOrders(
		ctx, shortOrders, longBook, TEST_PAIR(), false, dirtyIds, &settlements,
	)
	assert.Equal(t, totalPrice, uint64(500))
	assert.Equal(t, totalExecuted, uint64(5))
	assert.Equal(t, len(dirtyIds), 1)
	assert.Equal(t, dirtyIds[uint64(100)], true)
	assert.Equal(t, len(longBook), 1)
	assert.Equal(t, longBook[0].GetId(), uint64(100))
	assert.Equal(t, *longBook[0].GetEntry(), types.OrderEntry{
		Price:             uint64(100),
		Quantity:          uint64(0),
		AllocationCreator: []string{},
		Allocation:        []uint64{},
		PriceDenom:        TEST_PAIR().PriceDenom,
		AssetDenom:        TEST_PAIR().AssetDenom,
	})
	assert.Equal(t, len(settlements), 2)
	assert.Equal(t, *settlements[0], types.Settlement{
		Long:                   false,
		PriceSymbol:            TEST_PAIR().PriceDenom,
		AssetSymbol:            TEST_PAIR().AssetDenom,
		Quantity:               uint64(5),
		ExecutionCostOrProceed: uint64(100),
		ExpectedCostOrProceed:  uint64(100),
		Account:                "abc",
	})
	assert.Equal(t, *settlements[1], types.Settlement{
		Long:                   true,
		PriceSymbol:            TEST_PAIR().PriceDenom,
		AssetSymbol:            TEST_PAIR().AssetDenom,
		Quantity:               uint64(5),
		ExecutionCostOrProceed: uint64(100),
		ExpectedCostOrProceed:  uint64(100),
		Account:                "def",
	})
}

func TestMatchSingleMarketOrderFromMultipleShortBook(t *testing.T) {
	_, ctx := keepertest.DexKeeper(t)
	longOrders := []dexcache.MarketOrder{
		{
			WorstPrice: 100,
			Quantity:   5,
			Creator:    "abc",
			Long:       true,
		},
	}
	shortBook := []types.OrderBook{
		&types.ShortBook{
			Id: 90,
			Entry: &types.OrderEntry{
				Price:             90,
				Quantity:          2,
				AllocationCreator: []string{"def|c|"},
				Allocation:        []uint64{2},
				PriceDenom:        TEST_PAIR().PriceDenom,
				AssetDenom:        TEST_PAIR().AssetDenom,
			},
		},
		&types.ShortBook{
			Id: 100,
			Entry: &types.OrderEntry{
				Price:             100,
				Quantity:          6,
				AllocationCreator: []string{"def|c|", "ghi|c|"},
				Allocation:        []uint64{4, 2},
				PriceDenom:        TEST_PAIR().PriceDenom,
				AssetDenom:        TEST_PAIR().AssetDenom,
			},
		},
	}
	dirtyIds := map[uint64]bool{}
	settlements := []*types.Settlement{}
	totalPrice, totalExecuted := exchange.MatchMarketOrders(
		ctx, longOrders, shortBook, TEST_PAIR(), true, dirtyIds, &settlements,
	)
	assert.Equal(t, totalPrice, uint64(490))
	assert.Equal(t, totalExecuted, uint64(5))
	assert.Equal(t, len(dirtyIds), 2)
	assert.Equal(t, dirtyIds[uint64(90)], true)
	assert.Equal(t, dirtyIds[uint64(100)], true)
	assert.Equal(t, len(shortBook), 2)
	assert.Equal(t, shortBook[0].GetId(), uint64(90))
	assert.Equal(t, *shortBook[0].GetEntry(), types.OrderEntry{
		Price:             uint64(90),
		Quantity:          uint64(0),
		AllocationCreator: []string{},
		Allocation:        []uint64{},
		PriceDenom:        TEST_PAIR().PriceDenom,
		AssetDenom:        TEST_PAIR().AssetDenom,
	})
	assert.Equal(t, shortBook[1].GetId(), uint64(100))
	assert.Equal(t, *shortBook[1].GetEntry(), types.OrderEntry{
		Price:             uint64(100),
		Quantity:          uint64(3),
		AllocationCreator: []string{"def|c|", "ghi|c|"},
		Allocation:        []uint64{2, 1},
		PriceDenom:        TEST_PAIR().PriceDenom,
		AssetDenom:        TEST_PAIR().AssetDenom,
	})
	assert.Equal(t, len(settlements), 6)
	assert.Equal(t, *settlements[0], types.Settlement{
		Long:                   true,
		PriceSymbol:            TEST_PAIR().PriceDenom,
		AssetSymbol:            TEST_PAIR().AssetDenom,
		Quantity:               uint64(2),
		ExecutionCostOrProceed: uint64(95),
		ExpectedCostOrProceed:  uint64(100),
		Account:                "abc",
	})
	assert.Equal(t, *settlements[1], types.Settlement{
		Long:                   false,
		PriceSymbol:            TEST_PAIR().PriceDenom,
		AssetSymbol:            TEST_PAIR().AssetDenom,
		Quantity:               uint64(2),
		ExecutionCostOrProceed: uint64(95),
		ExpectedCostOrProceed:  uint64(90),
		Account:                "def",
	})
	assert.Equal(t, *settlements[2], types.Settlement{
		Long:                   true,
		PriceSymbol:            TEST_PAIR().PriceDenom,
		AssetSymbol:            TEST_PAIR().AssetDenom,
		Quantity:               uint64(2),
		ExecutionCostOrProceed: uint64(100),
		ExpectedCostOrProceed:  uint64(100),
		Account:                "abc",
	})
	assert.Equal(t, *settlements[3], types.Settlement{
		Long:                   false,
		PriceSymbol:            TEST_PAIR().PriceDenom,
		AssetSymbol:            TEST_PAIR().AssetDenom,
		Quantity:               uint64(2),
		ExecutionCostOrProceed: uint64(100),
		ExpectedCostOrProceed:  uint64(100),
		Account:                "def",
	})
	assert.Equal(t, *settlements[4], types.Settlement{
		Long:                   true,
		PriceSymbol:            TEST_PAIR().PriceDenom,
		AssetSymbol:            TEST_PAIR().AssetDenom,
		Quantity:               uint64(1),
		ExecutionCostOrProceed: uint64(100),
		ExpectedCostOrProceed:  uint64(100),
		Account:                "abc",
	})
	assert.Equal(t, *settlements[5], types.Settlement{
		Long:                   false,
		PriceSymbol:            TEST_PAIR().PriceDenom,
		AssetSymbol:            TEST_PAIR().AssetDenom,
		Quantity:               uint64(1),
		ExecutionCostOrProceed: uint64(100),
		ExpectedCostOrProceed:  uint64(100),
		Account:                "ghi",
	})
}

func TestMatchSingleMarketOrderFromMultipleLongBook(t *testing.T) {
	_, ctx := keepertest.DexKeeper(t)
	shortOrders := []dexcache.MarketOrder{
		{
			WorstPrice: 100,
			Quantity:   5,
			Creator:    "def",
			Long:       false,
		},
	}
	longBook := []types.OrderBook{
		&types.LongBook{
			Id: 100,
			Entry: &types.OrderEntry{
				Price:             100,
				Quantity:          6,
				AllocationCreator: []string{"abc|c|", "ghi|c|"},
				Allocation:        []uint64{4, 2},
				PriceDenom:        TEST_PAIR().PriceDenom,
				AssetDenom:        TEST_PAIR().AssetDenom,
			},
		},
		&types.LongBook{
			Id: 110,
			Entry: &types.OrderEntry{
				Price:             110,
				Quantity:          2,
				AllocationCreator: []string{"abc|c|"},
				Allocation:        []uint64{2},
				PriceDenom:        TEST_PAIR().PriceDenom,
				AssetDenom:        TEST_PAIR().AssetDenom,
			},
		},
	}
	dirtyIds := map[uint64]bool{}
	settlements := []*types.Settlement{}
	totalPrice, totalExecuted := exchange.MatchMarketOrders(
		ctx, shortOrders, longBook, TEST_PAIR(), false, dirtyIds, &settlements,
	)
	assert.Equal(t, totalPrice, uint64(510))
	assert.Equal(t, totalExecuted, uint64(5))
	assert.Equal(t, len(dirtyIds), 2)
	assert.Equal(t, dirtyIds[uint64(100)], true)
	assert.Equal(t, dirtyIds[uint64(110)], true)
	assert.Equal(t, len(longBook), 2)
	assert.Equal(t, longBook[0].GetId(), uint64(100))
	assert.Equal(t, *longBook[0].GetEntry(), types.OrderEntry{
		Price:             uint64(100),
		Quantity:          uint64(3),
		AllocationCreator: []string{"abc|c|", "ghi|c|"},
		Allocation:        []uint64{2, 1},
		PriceDenom:        TEST_PAIR().PriceDenom,
		AssetDenom:        TEST_PAIR().AssetDenom,
	})
	assert.Equal(t, longBook[1].GetId(), uint64(110))
	assert.Equal(t, *longBook[1].GetEntry(), types.OrderEntry{
		Price:             uint64(110),
		Quantity:          uint64(0),
		AllocationCreator: []string{},
		Allocation:        []uint64{},
		PriceDenom:        TEST_PAIR().PriceDenom,
		AssetDenom:        TEST_PAIR().AssetDenom,
	})
	assert.Equal(t, len(settlements), 6)
	assert.Equal(t, *settlements[0], types.Settlement{
		Long:                   false,
		PriceSymbol:            TEST_PAIR().PriceDenom,
		AssetSymbol:            TEST_PAIR().AssetDenom,
		Quantity:               uint64(2),
		ExecutionCostOrProceed: uint64(105),
		ExpectedCostOrProceed:  uint64(100),
		Account:                "def",
	})
	assert.Equal(t, *settlements[1], types.Settlement{
		Long:                   true,
		PriceSymbol:            TEST_PAIR().PriceDenom,
		AssetSymbol:            TEST_PAIR().AssetDenom,
		Quantity:               uint64(2),
		ExecutionCostOrProceed: uint64(105),
		ExpectedCostOrProceed:  uint64(110),
		Account:                "abc",
	})
	assert.Equal(t, *settlements[2], types.Settlement{
		Long:                   false,
		PriceSymbol:            TEST_PAIR().PriceDenom,
		AssetSymbol:            TEST_PAIR().AssetDenom,
		Quantity:               uint64(2),
		ExecutionCostOrProceed: uint64(100),
		ExpectedCostOrProceed:  uint64(100),
		Account:                "def",
	})
	assert.Equal(t, *settlements[3], types.Settlement{
		Long:                   true,
		PriceSymbol:            TEST_PAIR().PriceDenom,
		AssetSymbol:            TEST_PAIR().AssetDenom,
		Quantity:               uint64(2),
		ExecutionCostOrProceed: uint64(100),
		ExpectedCostOrProceed:  uint64(100),
		Account:                "abc",
	})
	assert.Equal(t, *settlements[4], types.Settlement{
		Long:                   false,
		PriceSymbol:            TEST_PAIR().PriceDenom,
		AssetSymbol:            TEST_PAIR().AssetDenom,
		Quantity:               uint64(1),
		ExecutionCostOrProceed: uint64(100),
		ExpectedCostOrProceed:  uint64(100),
		Account:                "def",
	})
	assert.Equal(t, *settlements[5], types.Settlement{
		Long:                   true,
		PriceSymbol:            TEST_PAIR().PriceDenom,
		AssetSymbol:            TEST_PAIR().AssetDenom,
		Quantity:               uint64(1),
		ExecutionCostOrProceed: uint64(100),
		ExpectedCostOrProceed:  uint64(100),
		Account:                "ghi",
	})
}

func TestMatchMultipleMarketOrderFromMultipleShortBook(t *testing.T) {
	_, ctx := keepertest.DexKeeper(t)
	longOrders := []dexcache.MarketOrder{
		{
			WorstPrice: 104,
			Quantity:   1,
			Creator:    "jkl",
			Long:       true,
		},
		{
			WorstPrice: 100,
			Quantity:   5,
			Creator:    "abc",
			Long:       true,
		},
		{
			WorstPrice: 98,
			Quantity:   2,
			Creator:    "mno",
			Long:       true,
		},
	}
	shortBook := []types.OrderBook{
		&types.ShortBook{
			Id: 90,
			Entry: &types.OrderEntry{
				Price:             90,
				Quantity:          2,
				AllocationCreator: []string{"def|c|"},
				Allocation:        []uint64{2},
				PriceDenom:        TEST_PAIR().PriceDenom,
				AssetDenom:        TEST_PAIR().AssetDenom,
			},
		},
		&types.ShortBook{
			Id: 100,
			Entry: &types.OrderEntry{
				Price:             100,
				Quantity:          6,
				AllocationCreator: []string{"def|c|", "ghi|c|"},
				Allocation:        []uint64{4, 2},
				PriceDenom:        TEST_PAIR().PriceDenom,
				AssetDenom:        TEST_PAIR().AssetDenom,
			},
		},
	}
	dirtyIds := map[uint64]bool{}
	settlements := []*types.Settlement{}
	totalPrice, totalExecuted := exchange.MatchMarketOrders(
		ctx, longOrders, shortBook, TEST_PAIR(), true, dirtyIds, &settlements,
	)
	assert.Equal(t, totalPrice, uint64(592))
	assert.Equal(t, totalExecuted, uint64(6))
	assert.Equal(t, len(dirtyIds), 2)
	assert.Equal(t, dirtyIds[uint64(90)], true)
	assert.Equal(t, dirtyIds[uint64(100)], true)
	assert.Equal(t, len(shortBook), 2)
	assert.Equal(t, shortBook[0].GetId(), uint64(90))
	assert.Equal(t, *shortBook[0].GetEntry(), types.OrderEntry{
		Price:             uint64(90),
		Quantity:          uint64(0),
		AllocationCreator: []string{},
		Allocation:        []uint64{},
		PriceDenom:        TEST_PAIR().PriceDenom,
		AssetDenom:        TEST_PAIR().AssetDenom,
	})
	assert.Equal(t, shortBook[1].GetId(), uint64(100))
	assert.Equal(t, *shortBook[1].GetEntry(), types.OrderEntry{
		Price:             uint64(100),
		Quantity:          uint64(2),
		AllocationCreator: []string{"def|c|"},
		Allocation:        []uint64{2},
		PriceDenom:        TEST_PAIR().PriceDenom,
		AssetDenom:        TEST_PAIR().AssetDenom,
	})
	assert.Equal(t, len(settlements), 8)
	assert.Equal(t, *settlements[0], types.Settlement{
		Long:                   true,
		PriceSymbol:            TEST_PAIR().PriceDenom,
		AssetSymbol:            TEST_PAIR().AssetDenom,
		Quantity:               uint64(1),
		ExecutionCostOrProceed: uint64(97),
		ExpectedCostOrProceed:  uint64(104),
		Account:                "jkl",
	})
	assert.Equal(t, *settlements[1], types.Settlement{
		Long:                   false,
		PriceSymbol:            TEST_PAIR().PriceDenom,
		AssetSymbol:            TEST_PAIR().AssetDenom,
		Quantity:               uint64(1),
		ExecutionCostOrProceed: uint64(97),
		ExpectedCostOrProceed:  uint64(90),
		Account:                "def",
	})
	assert.Equal(t, *settlements[2], types.Settlement{
		Long:                   true,
		PriceSymbol:            TEST_PAIR().PriceDenom,
		AssetSymbol:            TEST_PAIR().AssetDenom,
		Quantity:               uint64(1),
		ExecutionCostOrProceed: uint64(95),
		ExpectedCostOrProceed:  uint64(100),
		Account:                "abc",
	})
	assert.Equal(t, *settlements[3], types.Settlement{
		Long:                   false,
		PriceSymbol:            TEST_PAIR().PriceDenom,
		AssetSymbol:            TEST_PAIR().AssetDenom,
		Quantity:               uint64(1),
		ExecutionCostOrProceed: uint64(95),
		ExpectedCostOrProceed:  uint64(90),
		Account:                "def",
	})
	assert.Equal(t, *settlements[4], types.Settlement{
		Long:                   true,
		PriceSymbol:            TEST_PAIR().PriceDenom,
		AssetSymbol:            TEST_PAIR().AssetDenom,
		Quantity:               uint64(2),
		ExecutionCostOrProceed: uint64(100),
		ExpectedCostOrProceed:  uint64(100),
		Account:                "abc",
	})
	assert.Equal(t, *settlements[5], types.Settlement{
		Long:                   false,
		PriceSymbol:            TEST_PAIR().PriceDenom,
		AssetSymbol:            TEST_PAIR().AssetDenom,
		Quantity:               uint64(2),
		ExecutionCostOrProceed: uint64(100),
		ExpectedCostOrProceed:  uint64(100),
		Account:                "def",
	})
	assert.Equal(t, *settlements[6], types.Settlement{
		Long:                   true,
		PriceSymbol:            TEST_PAIR().PriceDenom,
		AssetSymbol:            TEST_PAIR().AssetDenom,
		Quantity:               uint64(2),
		ExecutionCostOrProceed: uint64(100),
		ExpectedCostOrProceed:  uint64(100),
		Account:                "abc",
	})
	assert.Equal(t, *settlements[7], types.Settlement{
		Long:                   false,
		PriceSymbol:            TEST_PAIR().PriceDenom,
		AssetSymbol:            TEST_PAIR().AssetDenom,
		Quantity:               uint64(2),
		ExecutionCostOrProceed: uint64(100),
		ExpectedCostOrProceed:  uint64(100),
		Account:                "ghi",
	})
}

func TestMatchMultipleMarketOrderFromMultipleLongBook(t *testing.T) {
	_, ctx := keepertest.DexKeeper(t)
	shortOrders := []dexcache.MarketOrder{
		{
			WorstPrice: 96,
			Quantity:   1,
			Creator:    "jkl",
			Long:       false,
		},
		{
			WorstPrice: 100,
			Quantity:   5,
			Creator:    "abc",
			Long:       false,
		},
		{
			WorstPrice: 102,
			Quantity:   2,
			Creator:    "mno",
			Long:       false,
		},
	}
	longBook := []types.OrderBook{
		&types.LongBook{
			Id: 100,
			Entry: &types.OrderEntry{
				Price:             100,
				Quantity:          6,
				AllocationCreator: []string{"abc|c|", "ghi|c|"},
				Allocation:        []uint64{4, 2},
				PriceDenom:        TEST_PAIR().PriceDenom,
				AssetDenom:        TEST_PAIR().AssetDenom,
			},
		},
		&types.LongBook{
			Id: 110,
			Entry: &types.OrderEntry{
				Price:             110,
				Quantity:          2,
				AllocationCreator: []string{"abc|c|"},
				Allocation:        []uint64{2},
				PriceDenom:        TEST_PAIR().PriceDenom,
				AssetDenom:        TEST_PAIR().AssetDenom,
			},
		},
	}
	dirtyIds := map[uint64]bool{}
	settlements := []*types.Settlement{}
	totalPrice, totalExecuted := exchange.MatchMarketOrders(
		ctx, shortOrders, longBook, TEST_PAIR(), false, dirtyIds, &settlements,
	)
	assert.Equal(t, totalPrice, uint64(608))
	assert.Equal(t, totalExecuted, uint64(6))
	assert.Equal(t, len(dirtyIds), 2)
	assert.Equal(t, dirtyIds[uint64(100)], true)
	assert.Equal(t, dirtyIds[uint64(110)], true)
	assert.Equal(t, len(longBook), 2)
	assert.Equal(t, longBook[0].GetId(), uint64(100))
	assert.Equal(t, *longBook[0].GetEntry(), types.OrderEntry{
		Price:             uint64(100),
		Quantity:          uint64(2),
		AllocationCreator: []string{"abc|c|"},
		Allocation:        []uint64{2},
		PriceDenom:        TEST_PAIR().PriceDenom,
		AssetDenom:        TEST_PAIR().AssetDenom,
	})
	assert.Equal(t, longBook[1].GetId(), uint64(110))
	assert.Equal(t, *longBook[1].GetEntry(), types.OrderEntry{
		Price:             uint64(110),
		Quantity:          uint64(0),
		AllocationCreator: []string{},
		Allocation:        []uint64{},
		PriceDenom:        TEST_PAIR().PriceDenom,
		AssetDenom:        TEST_PAIR().AssetDenom,
	})
	assert.Equal(t, len(settlements), 8)
	assert.Equal(t, *settlements[0], types.Settlement{
		Long:                   false,
		PriceSymbol:            TEST_PAIR().PriceDenom,
		AssetSymbol:            TEST_PAIR().AssetDenom,
		Quantity:               uint64(1),
		ExecutionCostOrProceed: uint64(103),
		ExpectedCostOrProceed:  uint64(96),
		Account:                "jkl",
	})
	assert.Equal(t, *settlements[1], types.Settlement{
		Long:                   true,
		PriceSymbol:            TEST_PAIR().PriceDenom,
		AssetSymbol:            TEST_PAIR().AssetDenom,
		Quantity:               uint64(1),
		ExecutionCostOrProceed: uint64(103),
		ExpectedCostOrProceed:  uint64(110),
		Account:                "abc",
	})
	assert.Equal(t, *settlements[2], types.Settlement{
		Long:                   false,
		PriceSymbol:            TEST_PAIR().PriceDenom,
		AssetSymbol:            TEST_PAIR().AssetDenom,
		Quantity:               uint64(1),
		ExecutionCostOrProceed: uint64(105),
		ExpectedCostOrProceed:  uint64(100),
		Account:                "abc",
	})
	assert.Equal(t, *settlements[3], types.Settlement{
		Long:                   true,
		PriceSymbol:            TEST_PAIR().PriceDenom,
		AssetSymbol:            TEST_PAIR().AssetDenom,
		Quantity:               uint64(1),
		ExecutionCostOrProceed: uint64(105),
		ExpectedCostOrProceed:  uint64(110),
		Account:                "abc",
	})
	assert.Equal(t, *settlements[4], types.Settlement{
		Long:                   false,
		PriceSymbol:            TEST_PAIR().PriceDenom,
		AssetSymbol:            TEST_PAIR().AssetDenom,
		Quantity:               uint64(2),
		ExecutionCostOrProceed: uint64(100),
		ExpectedCostOrProceed:  uint64(100),
		Account:                "abc",
	})
	assert.Equal(t, *settlements[5], types.Settlement{
		Long:                   true,
		PriceSymbol:            TEST_PAIR().PriceDenom,
		AssetSymbol:            TEST_PAIR().AssetDenom,
		Quantity:               uint64(2),
		ExecutionCostOrProceed: uint64(100),
		ExpectedCostOrProceed:  uint64(100),
		Account:                "abc",
	})
	assert.Equal(t, *settlements[6], types.Settlement{
		Long:                   false,
		PriceSymbol:            TEST_PAIR().PriceDenom,
		AssetSymbol:            TEST_PAIR().AssetDenom,
		Quantity:               uint64(2),
		ExecutionCostOrProceed: uint64(100),
		ExpectedCostOrProceed:  uint64(100),
		Account:                "abc",
	})
	assert.Equal(t, *settlements[7], types.Settlement{
		Long:                   true,
		PriceSymbol:            TEST_PAIR().PriceDenom,
		AssetSymbol:            TEST_PAIR().AssetDenom,
		Quantity:               uint64(2),
		ExecutionCostOrProceed: uint64(100),
		ExpectedCostOrProceed:  uint64(100),
		Account:                "ghi",
	})
}
