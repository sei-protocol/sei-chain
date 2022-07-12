package exchange_test

import (
	"testing"

	sdk "github.com/cosmos/cosmos-sdk/types"
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
	longOrders := []types.Order{
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
	shortOrders := []types.Order{
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
	longBook := []types.OrderBook{}
	shortBook := []types.OrderBook{}
	dirtyLongPrices := exchange.NewDirtyPrices()
	dirtyShortPrices := exchange.NewDirtyPrices()
	settlements := []*types.SettlementEntry{}
	totalPrice, totalExecuted := exchange.MatchLimitOrders(
		ctx, longOrders, shortOrders, &longBook, &shortBook, TEST_PAIR(), &dirtyLongPrices, &dirtyShortPrices, &settlements, &[]exchange.AccountOrderID{},
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
	})
}

func TestAddOrders(t *testing.T) {
	_, ctx := keepertest.DexKeeper(t)
	longOrders := []types.Order{
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
	shortOrders := []types.Order{
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
	longBook := []types.OrderBook{
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
	shortBook := []types.OrderBook{
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
	dirtyLongPrices := exchange.NewDirtyPrices()
	dirtyShortPrices := exchange.NewDirtyPrices()
	settlements := []*types.SettlementEntry{}
	totalPrice, totalExecuted := exchange.MatchLimitOrders(
		ctx, longOrders, shortOrders, &longBook, &shortBook, TEST_PAIR(), &dirtyLongPrices, &dirtyShortPrices, &settlements, &[]exchange.AccountOrderID{},
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
	longOrders := []types.Order{
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
	shortOrders := []types.Order{}
	longBook := []types.OrderBook{}
	shortBook := []types.OrderBook{
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
	dirtyLongPrices := exchange.NewDirtyPrices()
	dirtyShortPrices := exchange.NewDirtyPrices()
	settlements := []*types.SettlementEntry{}
	totalPrice, totalExecuted := exchange.MatchLimitOrders(
		ctx, longOrders, shortOrders, &longBook, &shortBook, TEST_PAIR(), &dirtyLongPrices, &dirtyShortPrices, &settlements, &[]exchange.AccountOrderID{},
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
	})
}

func TestMatchSingleOrderFromLongBook(t *testing.T) {
	_, ctx := keepertest.DexKeeper(t)
	longOrders := []types.Order{}
	shortOrders := []types.Order{
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
	longBook := []types.OrderBook{
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
	shortBook := []types.OrderBook{}
	dirtyLongPrices := exchange.NewDirtyPrices()
	dirtyShortPrices := exchange.NewDirtyPrices()
	settlements := []*types.SettlementEntry{}
	totalPrice, totalExecuted := exchange.MatchLimitOrders(
		ctx, longOrders, shortOrders, &longBook, &shortBook, TEST_PAIR(), &dirtyLongPrices, &dirtyShortPrices, &settlements, &[]exchange.AccountOrderID{},
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
	})
}

func TestMatchSingleOrderFromMultipleShortBook(t *testing.T) {
	_, ctx := keepertest.DexKeeper(t)
	longOrders := []types.Order{
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
	shortOrders := []types.Order{}
	longBook := []types.OrderBook{}
	shortBook := []types.OrderBook{
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
	dirtyLongPrices := exchange.NewDirtyPrices()
	dirtyShortPrices := exchange.NewDirtyPrices()
	settlements := []*types.SettlementEntry{}
	totalPrice, totalExecuted := exchange.MatchLimitOrders(
		ctx, longOrders, shortOrders, &longBook, &shortBook, TEST_PAIR(), &dirtyLongPrices, &dirtyShortPrices, &settlements, &[]exchange.AccountOrderID{},
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
	})
}

func TestMatchSingleOrderFromMultipleLongBook(t *testing.T) {
	_, ctx := keepertest.DexKeeper(t)
	longOrders := []types.Order{}
	shortOrders := []types.Order{
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
	longBook := []types.OrderBook{
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
	shortBook := []types.OrderBook{}
	dirtyLongPrices := exchange.NewDirtyPrices()
	dirtyShortPrices := exchange.NewDirtyPrices()
	settlements := []*types.SettlementEntry{}
	totalPrice, totalExecuted := exchange.MatchLimitOrders(
		ctx, longOrders, shortOrders, &longBook, &shortBook, TEST_PAIR(), &dirtyLongPrices, &dirtyShortPrices, &settlements, &[]exchange.AccountOrderID{},
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
	})
}

func TestMatchMultipleOrderFromMultipleShortBook(t *testing.T) {
	_, ctx := keepertest.DexKeeper(t)
	longOrders := []types.Order{
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
	shortOrders := []types.Order{}
	longBook := []types.OrderBook{}
	shortBook := []types.OrderBook{
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
	dirtyLongPrices := exchange.NewDirtyPrices()
	dirtyShortPrices := exchange.NewDirtyPrices()
	settlements := []*types.SettlementEntry{}
	totalPrice, totalExecuted := exchange.MatchLimitOrders(
		ctx, longOrders, shortOrders, &longBook, &shortBook, TEST_PAIR(), &dirtyLongPrices, &dirtyShortPrices, &settlements, &[]exchange.AccountOrderID{},
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
	})
}

func TestMatchMultipleOrderFromMultipleLongBook(t *testing.T) {
	_, ctx := keepertest.DexKeeper(t)
	longOrders := []types.Order{}
	shortOrders := []types.Order{
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
	longBook := []types.OrderBook{
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
	shortBook := []types.OrderBook{}
	dirtyLongPrices := exchange.NewDirtyPrices()
	dirtyShortPrices := exchange.NewDirtyPrices()
	settlements := []*types.SettlementEntry{}
	totalPrice, totalExecuted := exchange.MatchLimitOrders(
		ctx, longOrders, shortOrders, &longBook, &shortBook, TEST_PAIR(), &dirtyLongPrices, &dirtyShortPrices, &settlements, &[]exchange.AccountOrderID{},
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
	})
}
