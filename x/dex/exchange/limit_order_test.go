package exchange_test

import (
	"testing"

	dexcache "github.com/sei-protocol/sei-chain/x/dex/cache"
	"github.com/sei-protocol/sei-chain/x/dex/exchange"
	"github.com/sei-protocol/sei-chain/x/dex/types"
	"github.com/stretchr/testify/assert"

	keepertest "github.com/sei-protocol/sei-chain/testutil/keeper"
)

func TEST_PAIR() types.Pair {
	return types.Pair{
		PriceDenom: "ust",
		AssetDenom: "luna",
	}
}

func TestMatchSingleOrder(t *testing.T) {
	_, ctx := keepertest.DexKeeper(t)
	longOrders := []dexcache.LimitOrder{
		{
			Price:    100,
			Quantity: 5,
			Creator:  "abc",
			Long:     true,
		},
	}
	shortOrders := []dexcache.LimitOrder{
		{
			Price:    100,
			Quantity: 5,
			Creator:  "def",
			Long:     false,
		},
	}
	longBook := []types.OrderBook{}
	shortBook := []types.OrderBook{}
	dirtyLongIds := map[uint64]bool{}
	dirtyShortIds := map[uint64]bool{}
	settlements := []*types.Settlement{}
	totalPrice, totalExecuted := exchange.MatchLimitOrders(
		ctx, longOrders, shortOrders, &longBook, &shortBook, TEST_PAIR(), dirtyLongIds, dirtyShortIds, &settlements,
	)
	assert.Equal(t, totalPrice, uint64(1000))
	assert.Equal(t, totalExecuted, uint64(10))
	assert.Equal(t, len(dirtyLongIds), 1)
	assert.Equal(t, len(dirtyShortIds), 1)
	assert.Equal(t, dirtyLongIds[uint64(100)], true)
	assert.Equal(t, dirtyShortIds[uint64(100)], true)
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

func TestAddOrders(t *testing.T) {
	_, ctx := keepertest.DexKeeper(t)
	longOrders := []dexcache.LimitOrder{
		{
			Price:    100,
			Quantity: 5,
			Creator:  "def",
			Long:     true,
		},
		{
			Price:    95,
			Quantity: 3,
			Creator:  "def",
			Long:     true,
		},
	}
	shortOrders := []dexcache.LimitOrder{
		{
			Price:    105,
			Quantity: 10,
			Creator:  "ghi",
			Long:     false,
			Leverage: "2",
		},
		{
			Price:    115,
			Quantity: 2,
			Creator:  "mno",
			Long:     false,
		},
	}
	longBook := []types.OrderBook{
		&types.LongBook{
			Id: 98,
			Entry: &types.OrderEntry{
				Price:             98,
				Quantity:          5,
				AllocationCreator: []string{"abc|c|"},
				Allocation:        []uint64{5},
				PriceDenom:        TEST_PAIR().PriceDenom,
				AssetDenom:        TEST_PAIR().AssetDenom,
			},
		},
		&types.LongBook{
			Id: 100,
			Entry: &types.OrderEntry{
				Price:             100,
				Quantity:          3,
				AllocationCreator: []string{"def|c|"},
				Allocation:        []uint64{3},
				PriceDenom:        TEST_PAIR().PriceDenom,
				AssetDenom:        TEST_PAIR().AssetDenom,
			},
		},
	}
	shortBook := []types.OrderBook{
		&types.ShortBook{
			Id: 101,
			Entry: &types.OrderEntry{
				Price:             101,
				Quantity:          5,
				AllocationCreator: []string{"abc|c|"},
				Allocation:        []uint64{5},
				PriceDenom:        TEST_PAIR().PriceDenom,
				AssetDenom:        TEST_PAIR().AssetDenom,
			},
		},
		&types.ShortBook{
			Id: 115,
			Entry: &types.OrderEntry{
				Price:             115,
				Quantity:          3,
				AllocationCreator: []string{"def|c|"},
				Allocation:        []uint64{3},
				PriceDenom:        TEST_PAIR().PriceDenom,
				AssetDenom:        TEST_PAIR().AssetDenom,
			},
		},
	}
	dirtyLongIds := map[uint64]bool{}
	dirtyShortIds := map[uint64]bool{}
	settlements := []*types.Settlement{}
	totalPrice, totalExecuted := exchange.MatchLimitOrders(
		ctx, longOrders, shortOrders, &longBook, &shortBook, TEST_PAIR(), dirtyLongIds, dirtyShortIds, &settlements,
	)
	assert.Equal(t, totalPrice, uint64(0))
	assert.Equal(t, totalExecuted, uint64(0))
	assert.Equal(t, len(dirtyLongIds), 2)
	assert.Equal(t, len(dirtyShortIds), 2)
	assert.Equal(t, dirtyLongIds[uint64(95)], true)
	assert.Equal(t, dirtyLongIds[uint64(100)], true)
	assert.Equal(t, dirtyShortIds[uint64(105)], true)
	assert.Equal(t, dirtyShortIds[uint64(115)], true)
	assert.Equal(t, len(longBook), 3)
	assert.Equal(t, longBook[0].GetId(), uint64(95))
	assert.Equal(t, *longBook[0].GetEntry(), types.OrderEntry{
		Price:             uint64(95),
		Quantity:          uint64(3),
		AllocationCreator: []string{"def|c|"},
		Allocation:        []uint64{3},
		PriceDenom:        TEST_PAIR().PriceDenom,
		AssetDenom:        TEST_PAIR().AssetDenom,
	})
	assert.Equal(t, longBook[1].GetId(), uint64(98))
	assert.Equal(t, *longBook[1].GetEntry(), types.OrderEntry{
		Price:             uint64(98),
		Quantity:          uint64(5),
		AllocationCreator: []string{"abc|c|"},
		Allocation:        []uint64{5},
		PriceDenom:        TEST_PAIR().PriceDenom,
		AssetDenom:        TEST_PAIR().AssetDenom,
	})
	assert.Equal(t, longBook[2].GetId(), uint64(100))
	assert.Equal(t, *longBook[2].GetEntry(), types.OrderEntry{
		Price:             uint64(100),
		Quantity:          uint64(8),
		AllocationCreator: []string{"def|c|"},
		Allocation:        []uint64{8},
		PriceDenom:        TEST_PAIR().PriceDenom,
		AssetDenom:        TEST_PAIR().AssetDenom,
	})
	assert.Equal(t, len(shortBook), 3)
	assert.Equal(t, shortBook[0].GetId(), uint64(101))
	assert.Equal(t, *shortBook[0].GetEntry(), types.OrderEntry{
		Price:             uint64(101),
		Quantity:          uint64(5),
		AllocationCreator: []string{"abc|c|"},
		Allocation:        []uint64{5},
		PriceDenom:        TEST_PAIR().PriceDenom,
		AssetDenom:        TEST_PAIR().AssetDenom,
	})
	assert.Equal(t, shortBook[1].GetId(), uint64(105))
	assert.Equal(t, *shortBook[1].GetEntry(), types.OrderEntry{
		Price:             uint64(105),
		Quantity:          uint64(10),
		AllocationCreator: []string{"ghi|c|2"},
		Allocation:        []uint64{10},
		PriceDenom:        TEST_PAIR().PriceDenom,
		AssetDenom:        TEST_PAIR().AssetDenom,
	})
	assert.Equal(t, shortBook[2].GetId(), uint64(115))
	assert.Equal(t, *shortBook[2].GetEntry(), types.OrderEntry{
		Price:             uint64(115),
		Quantity:          uint64(5),
		AllocationCreator: []string{"def|c|", "mno|c|"},
		Allocation:        []uint64{3, 2},
		PriceDenom:        TEST_PAIR().PriceDenom,
		AssetDenom:        TEST_PAIR().AssetDenom,
	})
	assert.Equal(t, len(settlements), 0)
}

func TestMatchSingleOrderFromShortBook(t *testing.T) {
	_, ctx := keepertest.DexKeeper(t)
	longOrders := []dexcache.LimitOrder{
		{
			Price:    100,
			Quantity: 5,
			Creator:  "abc",
			Long:     true,
		},
	}
	shortOrders := []dexcache.LimitOrder{}
	longBook := []types.OrderBook{}
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
	dirtyLongIds := map[uint64]bool{}
	dirtyShortIds := map[uint64]bool{}
	settlements := []*types.Settlement{}
	totalPrice, totalExecuted := exchange.MatchLimitOrders(
		ctx, longOrders, shortOrders, &longBook, &shortBook, TEST_PAIR(), dirtyLongIds, dirtyShortIds, &settlements,
	)
	assert.Equal(t, totalPrice, uint64(1000))
	assert.Equal(t, totalExecuted, uint64(10))
	assert.Equal(t, len(dirtyLongIds), 1)
	assert.Equal(t, len(dirtyShortIds), 1)
	assert.Equal(t, dirtyLongIds[uint64(100)], true)
	assert.Equal(t, dirtyShortIds[uint64(100)], true)
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

func TestMatchSingleOrderFromLongBook(t *testing.T) {
	_, ctx := keepertest.DexKeeper(t)
	longOrders := []dexcache.LimitOrder{}
	shortOrders := []dexcache.LimitOrder{
		{
			Price:    100,
			Quantity: 5,
			Creator:  "def",
			Long:     false,
		},
	}
	longBook := []types.OrderBook{
		&types.LongBook{
			Id: 100,
			Entry: &types.OrderEntry{
				Price:             100,
				Quantity:          5,
				AllocationCreator: []string{"abc|c|"},
				Allocation:        []uint64{5},
				PriceDenom:        TEST_PAIR().PriceDenom,
				AssetDenom:        TEST_PAIR().AssetDenom,
			},
		},
	}
	shortBook := []types.OrderBook{}
	dirtyLongIds := map[uint64]bool{}
	dirtyShortIds := map[uint64]bool{}
	settlements := []*types.Settlement{}
	totalPrice, totalExecuted := exchange.MatchLimitOrders(
		ctx, longOrders, shortOrders, &longBook, &shortBook, TEST_PAIR(), dirtyLongIds, dirtyShortIds, &settlements,
	)
	assert.Equal(t, totalPrice, uint64(1000))
	assert.Equal(t, totalExecuted, uint64(10))
	assert.Equal(t, len(dirtyLongIds), 1)
	assert.Equal(t, len(dirtyShortIds), 1)
	assert.Equal(t, dirtyLongIds[uint64(100)], true)
	assert.Equal(t, dirtyShortIds[uint64(100)], true)
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

func TestMatchSingleOrderFromMultipleShortBook(t *testing.T) {
	_, ctx := keepertest.DexKeeper(t)
	longOrders := []dexcache.LimitOrder{
		{
			Price:    100,
			Quantity: 5,
			Creator:  "abc",
			Long:     true,
		},
	}
	shortOrders := []dexcache.LimitOrder{}
	longBook := []types.OrderBook{}
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
	dirtyLongIds := map[uint64]bool{}
	dirtyShortIds := map[uint64]bool{}
	settlements := []*types.Settlement{}
	totalPrice, totalExecuted := exchange.MatchLimitOrders(
		ctx, longOrders, shortOrders, &longBook, &shortBook, TEST_PAIR(), dirtyLongIds, dirtyShortIds, &settlements,
	)
	assert.Equal(t, totalPrice, uint64(980))
	assert.Equal(t, totalExecuted, uint64(10))
	assert.Equal(t, len(dirtyLongIds), 1)
	assert.Equal(t, len(dirtyShortIds), 2)
	assert.Equal(t, dirtyLongIds[uint64(100)], true)
	assert.Equal(t, dirtyShortIds[uint64(90)], true)
	assert.Equal(t, dirtyShortIds[uint64(100)], true)
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

func TestMatchSingleOrderFromMultipleLongBook(t *testing.T) {
	_, ctx := keepertest.DexKeeper(t)
	longOrders := []dexcache.LimitOrder{}
	shortOrders := []dexcache.LimitOrder{
		{
			Price:    100,
			Quantity: 5,
			Creator:  "def",
			Long:     false,
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
	shortBook := []types.OrderBook{}
	dirtyLongIds := map[uint64]bool{}
	dirtyShortIds := map[uint64]bool{}
	settlements := []*types.Settlement{}
	totalPrice, totalExecuted := exchange.MatchLimitOrders(
		ctx, longOrders, shortOrders, &longBook, &shortBook, TEST_PAIR(), dirtyLongIds, dirtyShortIds, &settlements,
	)
	assert.Equal(t, totalPrice, uint64(1020))
	assert.Equal(t, totalExecuted, uint64(10))
	assert.Equal(t, len(dirtyLongIds), 2)
	assert.Equal(t, len(dirtyShortIds), 1)
	assert.Equal(t, dirtyLongIds[uint64(100)], true)
	assert.Equal(t, dirtyLongIds[uint64(110)], true)
	assert.Equal(t, dirtyShortIds[uint64(100)], true)
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
	assert.Equal(t, len(settlements), 6)
	assert.Equal(t, *settlements[0], types.Settlement{
		Long:                   true,
		PriceSymbol:            TEST_PAIR().PriceDenom,
		AssetSymbol:            TEST_PAIR().AssetDenom,
		Quantity:               uint64(2),
		ExecutionCostOrProceed: uint64(105),
		ExpectedCostOrProceed:  uint64(110),
		Account:                "abc",
	})
	assert.Equal(t, *settlements[1], types.Settlement{
		Long:                   false,
		PriceSymbol:            TEST_PAIR().PriceDenom,
		AssetSymbol:            TEST_PAIR().AssetDenom,
		Quantity:               uint64(2),
		ExecutionCostOrProceed: uint64(105),
		ExpectedCostOrProceed:  uint64(100),
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
		Account:                "ghi",
	})
	assert.Equal(t, *settlements[5], types.Settlement{
		Long:                   false,
		PriceSymbol:            TEST_PAIR().PriceDenom,
		AssetSymbol:            TEST_PAIR().AssetDenom,
		Quantity:               uint64(1),
		ExecutionCostOrProceed: uint64(100),
		ExpectedCostOrProceed:  uint64(100),
		Account:                "def",
	})
}

func TestMatchMultipleOrderFromMultipleShortBook(t *testing.T) {
	_, ctx := keepertest.DexKeeper(t)
	longOrders := []dexcache.LimitOrder{
		{
			Price:    100,
			Quantity: 5,
			Creator:  "abc",
			Long:     true,
		},
		{
			Price:    104,
			Quantity: 1,
			Creator:  "jkl",
			Long:     true,
		},
		{
			Price:    98,
			Quantity: 2,
			Creator:  "mno",
			Long:     true,
		},
	}
	shortOrders := []dexcache.LimitOrder{}
	longBook := []types.OrderBook{}
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
	dirtyLongIds := map[uint64]bool{}
	dirtyShortIds := map[uint64]bool{}
	settlements := []*types.Settlement{}
	totalPrice, totalExecuted := exchange.MatchLimitOrders(
		ctx, longOrders, shortOrders, &longBook, &shortBook, TEST_PAIR(), dirtyLongIds, dirtyShortIds, &settlements,
	)
	assert.Equal(t, totalPrice, uint64(1184))
	assert.Equal(t, totalExecuted, uint64(12))
	assert.Equal(t, len(dirtyLongIds), 3)
	assert.Equal(t, len(dirtyShortIds), 2)
	assert.Equal(t, dirtyLongIds[uint64(98)], true)
	assert.Equal(t, dirtyLongIds[uint64(100)], true)
	assert.Equal(t, dirtyLongIds[uint64(104)], true)
	assert.Equal(t, dirtyShortIds[uint64(90)], true)
	assert.Equal(t, dirtyShortIds[uint64(100)], true)
	assert.Equal(t, len(longBook), 3)
	assert.Equal(t, longBook[0].GetId(), uint64(98))
	assert.Equal(t, *longBook[0].GetEntry(), types.OrderEntry{
		Price:             uint64(98),
		Quantity:          uint64(2),
		AllocationCreator: []string{"mno|c|"},
		Allocation:        []uint64{2},
		PriceDenom:        TEST_PAIR().PriceDenom,
		AssetDenom:        TEST_PAIR().AssetDenom,
	})
	assert.Equal(t, longBook[1].GetId(), uint64(100))
	assert.Equal(t, *longBook[1].GetEntry(), types.OrderEntry{
		Price:             uint64(100),
		Quantity:          uint64(0),
		AllocationCreator: []string{},
		Allocation:        []uint64{},
		PriceDenom:        TEST_PAIR().PriceDenom,
		AssetDenom:        TEST_PAIR().AssetDenom,
	})
	assert.Equal(t, longBook[2].GetId(), uint64(104))
	assert.Equal(t, *longBook[2].GetEntry(), types.OrderEntry{
		Price:             uint64(104),
		Quantity:          uint64(0),
		AllocationCreator: []string{},
		Allocation:        []uint64{},
		PriceDenom:        TEST_PAIR().PriceDenom,
		AssetDenom:        TEST_PAIR().AssetDenom,
	})
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

func TestMatchMultipleOrderFromMultipleLongBook(t *testing.T) {
	_, ctx := keepertest.DexKeeper(t)
	longOrders := []dexcache.LimitOrder{}
	shortOrders := []dexcache.LimitOrder{
		{
			Price:    100,
			Quantity: 5,
			Creator:  "abc",
			Long:     false,
		},
		{
			Price:    96,
			Quantity: 1,
			Creator:  "jkl",
			Long:     false,
		},
		{
			Price:    102,
			Quantity: 2,
			Creator:  "mno",
			Long:     false,
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
	shortBook := []types.OrderBook{}
	dirtyLongIds := map[uint64]bool{}
	dirtyShortIds := map[uint64]bool{}
	settlements := []*types.Settlement{}
	totalPrice, totalExecuted := exchange.MatchLimitOrders(
		ctx, longOrders, shortOrders, &longBook, &shortBook, TEST_PAIR(), dirtyLongIds, dirtyShortIds, &settlements,
	)
	assert.Equal(t, totalPrice, uint64(1216))
	assert.Equal(t, totalExecuted, uint64(12))
	assert.Equal(t, len(dirtyLongIds), 2)
	assert.Equal(t, len(dirtyShortIds), 3)
	assert.Equal(t, dirtyLongIds[uint64(100)], true)
	assert.Equal(t, dirtyLongIds[uint64(110)], true)
	assert.Equal(t, dirtyShortIds[uint64(96)], true)
	assert.Equal(t, dirtyShortIds[uint64(100)], true)
	assert.Equal(t, dirtyShortIds[uint64(102)], true)
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
	assert.Equal(t, len(shortBook), 3)
	assert.Equal(t, shortBook[0].GetId(), uint64(96))
	assert.Equal(t, *shortBook[0].GetEntry(), types.OrderEntry{
		Price:             uint64(96),
		Quantity:          uint64(0),
		AllocationCreator: []string{},
		Allocation:        []uint64{},
		PriceDenom:        TEST_PAIR().PriceDenom,
		AssetDenom:        TEST_PAIR().AssetDenom,
	})
	assert.Equal(t, shortBook[1].GetId(), uint64(100))
	assert.Equal(t, *shortBook[1].GetEntry(), types.OrderEntry{
		Price:             uint64(100),
		Quantity:          uint64(0),
		AllocationCreator: []string{},
		Allocation:        []uint64{},
		PriceDenom:        TEST_PAIR().PriceDenom,
		AssetDenom:        TEST_PAIR().AssetDenom,
	})
	assert.Equal(t, shortBook[2].GetId(), uint64(102))
	assert.Equal(t, *shortBook[2].GetEntry(), types.OrderEntry{
		Price:             uint64(102),
		Quantity:          uint64(2),
		AllocationCreator: []string{"mno|c|"},
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
		ExecutionCostOrProceed: uint64(103),
		ExpectedCostOrProceed:  uint64(110),
		Account:                "abc",
	})
	assert.Equal(t, *settlements[1], types.Settlement{
		Long:                   false,
		PriceSymbol:            TEST_PAIR().PriceDenom,
		AssetSymbol:            TEST_PAIR().AssetDenom,
		Quantity:               uint64(1),
		ExecutionCostOrProceed: uint64(103),
		ExpectedCostOrProceed:  uint64(96),
		Account:                "jkl",
	})
	assert.Equal(t, *settlements[2], types.Settlement{
		Long:                   true,
		PriceSymbol:            TEST_PAIR().PriceDenom,
		AssetSymbol:            TEST_PAIR().AssetDenom,
		Quantity:               uint64(1),
		ExecutionCostOrProceed: uint64(105),
		ExpectedCostOrProceed:  uint64(110),
		Account:                "abc",
	})
	assert.Equal(t, *settlements[3], types.Settlement{
		Long:                   false,
		PriceSymbol:            TEST_PAIR().PriceDenom,
		AssetSymbol:            TEST_PAIR().AssetDenom,
		Quantity:               uint64(1),
		ExecutionCostOrProceed: uint64(105),
		ExpectedCostOrProceed:  uint64(100),
		Account:                "abc",
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
		Account:                "abc",
	})
	assert.Equal(t, *settlements[6], types.Settlement{
		Long:                   true,
		PriceSymbol:            TEST_PAIR().PriceDenom,
		AssetSymbol:            TEST_PAIR().AssetDenom,
		Quantity:               uint64(2),
		ExecutionCostOrProceed: uint64(100),
		ExpectedCostOrProceed:  uint64(100),
		Account:                "ghi",
	})
	assert.Equal(t, *settlements[7], types.Settlement{
		Long:                   false,
		PriceSymbol:            TEST_PAIR().PriceDenom,
		AssetSymbol:            TEST_PAIR().AssetDenom,
		Quantity:               uint64(2),
		ExecutionCostOrProceed: uint64(100),
		ExpectedCostOrProceed:  uint64(100),
		Account:                "abc",
	})
}
