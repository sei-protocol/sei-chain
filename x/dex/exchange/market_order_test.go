package exchange_test

import (
	"testing"

	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/sei-protocol/sei-chain/x/dex/exchange"
	"github.com/sei-protocol/sei-chain/x/dex/types"
	"github.com/stretchr/testify/assert"

	keepertest "github.com/sei-protocol/sei-chain/testutil/keeper"
)

func TestMatchSingleMarketOrderFromShortBook(t *testing.T) {
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
			OrderType:         types.OrderType_MARKET,
		},
	}
	shortBook := []types.OrderBook{
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
	dirtyPrices := exchange.NewDirtyPrices()
	settlements := []*types.SettlementEntry{}
	totalPrice, totalExecuted := exchange.MatchMarketOrders(
		ctx, longOrders, shortBook, TEST_PAIR(), types.PositionDirection_LONG, &dirtyPrices, &settlements, &[]exchange.AccountOrderID{},
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
	})
}

func TestMatchSingleMarketOrderFromLongBook(t *testing.T) {
	_, ctx := keepertest.DexKeeper(t)
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
			OrderType:         types.OrderType_MARKET,
		},
	}
	longBook := []types.OrderBook{
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
	dirtyPrices := exchange.NewDirtyPrices()
	settlements := []*types.SettlementEntry{}
	totalPrice, totalExecuted := exchange.MatchMarketOrders(
		ctx, shortOrders, longBook, TEST_PAIR(), types.PositionDirection_SHORT, &dirtyPrices, &settlements, &[]exchange.AccountOrderID{},
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
	})
}

func TestMatchSingleMarketOrderFromMultipleShortBook(t *testing.T) {
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
			OrderType:         types.OrderType_MARKET,
		},
	}
	shortBook := []types.OrderBook{
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
	dirtyPrices := exchange.NewDirtyPrices()
	settlements := []*types.SettlementEntry{}
	totalPrice, totalExecuted := exchange.MatchMarketOrders(
		ctx, longOrders, shortBook, TEST_PAIR(), types.PositionDirection_LONG, &dirtyPrices, &settlements, &[]exchange.AccountOrderID{},
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
		Price:    sdk.NewDec(100),
		Quantity: sdk.NewDec(3),
		Allocations: []*types.Allocation{{
			OrderId:  6,
			Account:  "def",
			Quantity: sdk.NewDec(2),
		}, {
			OrderId:  7,
			Account:  "ghi",
			Quantity: sdk.NewDec(1),
		}},
		PriceDenom: "USDC",
		AssetDenom: "ATOM",
	})
	assert.Equal(t, len(settlements), 6)
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
	})
	assert.Equal(t, *settlements[1], types.SettlementEntry{
		PositionDirection:      "Short",
		PriceDenom:             "USDC",
		AssetDenom:             "ATOM",
		Quantity:               sdk.NewDec(2),
		ExecutionCostOrProceed: sdk.NewDec(100),
		ExpectedCostOrProceed:  sdk.NewDec(100),
		Account:                "def",
		OrderType:              "Limit",
		OrderId:                6,
	})
	assert.Equal(t, *settlements[2], types.SettlementEntry{
		PositionDirection:      "Short",
		PriceDenom:             "USDC",
		AssetDenom:             "ATOM",
		Quantity:               sdk.NewDec(1),
		ExecutionCostOrProceed: sdk.NewDec(100),
		ExpectedCostOrProceed:  sdk.NewDec(100),
		Account:                "ghi",
		OrderType:              "Limit",
		OrderId:                7,
	})
	assert.Equal(t, *settlements[3], types.SettlementEntry{
		PositionDirection:      "Long",
		PriceDenom:             "USDC",
		AssetDenom:             "ATOM",
		Quantity:               sdk.NewDec(2),
		ExecutionCostOrProceed: sdk.NewDec(96),
		ExpectedCostOrProceed:  sdk.NewDec(100),
		Account:                "abc",
		OrderType:              "Market",
		OrderId:                1,
	})
	assert.Equal(t, *settlements[4], types.SettlementEntry{
		PositionDirection:      "Long",
		PriceDenom:             "USDC",
		AssetDenom:             "ATOM",
		Quantity:               sdk.NewDec(2),
		ExecutionCostOrProceed: sdk.NewDec(96),
		ExpectedCostOrProceed:  sdk.NewDec(100),
		Account:                "abc",
		OrderType:              "Market",
		OrderId:                1,
	})
	assert.Equal(t, *settlements[5], types.SettlementEntry{
		PositionDirection:      "Long",
		PriceDenom:             "USDC",
		AssetDenom:             "ATOM",
		Quantity:               sdk.NewDec(1),
		ExecutionCostOrProceed: sdk.NewDec(96),
		ExpectedCostOrProceed:  sdk.NewDec(100),
		Account:                "abc",
		OrderType:              "Market",
		OrderId:                1,
	})
}

func TestMatchSingleMarketOrderFromMultipleLongBook(t *testing.T) {
	_, ctx := keepertest.DexKeeper(t)
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
			OrderType:         types.OrderType_MARKET,
		},
	}
	longBook := []types.OrderBook{
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
	dirtyPrices := exchange.NewDirtyPrices()
	settlements := []*types.SettlementEntry{}
	totalPrice, totalExecuted := exchange.MatchMarketOrders(
		ctx, shortOrders, longBook, TEST_PAIR(), types.PositionDirection_SHORT, &dirtyPrices, &settlements, &[]exchange.AccountOrderID{},
	)
	assert.Equal(t, totalPrice, sdk.NewDec(520))
	assert.Equal(t, totalExecuted, sdk.NewDec(5))
	assert.Equal(t, len(dirtyPrices.Get()), 2)
	assert.Equal(t, dirtyPrices.Has(sdk.NewDec(100)), true)
	assert.Equal(t, dirtyPrices.Has(sdk.NewDec(110)), true)
	assert.Equal(t, len(longBook), 2)
	assert.Equal(t, longBook[0].GetPrice(), sdk.NewDec(100))
	assert.Equal(t, *longBook[0].GetEntry(), types.OrderEntry{
		Price:    sdk.NewDec(100),
		Quantity: sdk.NewDec(3),
		Allocations: []*types.Allocation{{
			OrderId:  6,
			Account:  "abc",
			Quantity: sdk.NewDec(2),
		}, {
			OrderId:  7,
			Account:  "ghi",
			Quantity: sdk.NewDec(1),
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
		Quantity:               sdk.NewDec(2),
		ExecutionCostOrProceed: sdk.NewDec(110),
		ExpectedCostOrProceed:  sdk.NewDec(110),
		Account:                "abc",
		OrderType:              "Limit",
		OrderId:                5,
	})
	assert.Equal(t, *settlements[1], types.SettlementEntry{
		PositionDirection:      "Long",
		PriceDenom:             "USDC",
		AssetDenom:             "ATOM",
		Quantity:               sdk.NewDec(2),
		ExecutionCostOrProceed: sdk.NewDec(100),
		ExpectedCostOrProceed:  sdk.NewDec(100),
		Account:                "abc",
		OrderType:              "Limit",
		OrderId:                6,
	})
	assert.Equal(t, *settlements[2], types.SettlementEntry{
		PositionDirection:      "Long",
		PriceDenom:             "USDC",
		AssetDenom:             "ATOM",
		Quantity:               sdk.NewDec(1),
		ExecutionCostOrProceed: sdk.NewDec(100),
		ExpectedCostOrProceed:  sdk.NewDec(100),
		Account:                "ghi",
		OrderType:              "Limit",
		OrderId:                7,
	})
	assert.Equal(t, *settlements[3], types.SettlementEntry{
		PositionDirection:      "Short",
		PriceDenom:             "USDC",
		AssetDenom:             "ATOM",
		Quantity:               sdk.NewDec(2),
		ExecutionCostOrProceed: sdk.NewDec(104),
		ExpectedCostOrProceed:  sdk.NewDec(100),
		Account:                "def",
		OrderType:              "Market",
		OrderId:                1,
	})
	assert.Equal(t, *settlements[4], types.SettlementEntry{
		PositionDirection:      "Short",
		PriceDenom:             "USDC",
		AssetDenom:             "ATOM",
		Quantity:               sdk.NewDec(2),
		ExecutionCostOrProceed: sdk.NewDec(104),
		ExpectedCostOrProceed:  sdk.NewDec(100),
		Account:                "def",
		OrderType:              "Market",
		OrderId:                1,
	})
	assert.Equal(t, *settlements[5], types.SettlementEntry{
		PositionDirection:      "Short",
		PriceDenom:             "USDC",
		AssetDenom:             "ATOM",
		Quantity:               sdk.NewDec(1),
		ExecutionCostOrProceed: sdk.NewDec(104),
		ExpectedCostOrProceed:  sdk.NewDec(100),
		Account:                "def",
		OrderType:              "Market",
		OrderId:                1,
	})
}

func TestMatchMultipleMarketOrderFromMultipleShortBook(t *testing.T) {
	_, ctx := keepertest.DexKeeper(t)
	longOrders := []types.Order{
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
	dirtyPrices := exchange.NewDirtyPrices()
	settlements := []*types.SettlementEntry{}
	totalPrice, totalExecuted := exchange.MatchMarketOrders(
		ctx, longOrders, shortBook, TEST_PAIR(), types.PositionDirection_LONG, &dirtyPrices, &settlements, &[]exchange.AccountOrderID{},
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
		PositionDirection:      "Short",
		PriceDenom:             "USDC",
		AssetDenom:             "ATOM",
		Quantity:               sdk.NewDec(1),
		ExecutionCostOrProceed: sdk.NewDec(90),
		ExpectedCostOrProceed:  sdk.NewDec(90),
		Account:                "def",
		OrderType:              "Limit",
		OrderId:                4,
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
	})
	assert.Equal(t, *settlements[2], types.SettlementEntry{
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
	assert.Equal(t, *settlements[3], types.SettlementEntry{
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
	assert.Equal(t, *settlements[4], types.SettlementEntry{
		PositionDirection:      "Long",
		PriceDenom:             "USDC",
		AssetDenom:             "ATOM",
		Quantity:               sdk.NewDec(1),
		ExecutionCostOrProceed: sdk.MustNewDecFromStr("96.666666666666666667"),
		ExpectedCostOrProceed:  sdk.NewDec(104),
		Account:                "jkl",
		OrderType:              "Market",
		OrderId:                1,
	})
	assert.Equal(t, *settlements[5], types.SettlementEntry{
		PositionDirection:      "Long",
		PriceDenom:             "USDC",
		AssetDenom:             "ATOM",
		Quantity:               sdk.NewDec(1),
		ExecutionCostOrProceed: sdk.MustNewDecFromStr("96.666666666666666667"),
		ExpectedCostOrProceed:  sdk.NewDec(100),
		Account:                "abc",
		OrderType:              "Market",
		OrderId:                2,
	})
	assert.Equal(t, *settlements[6], types.SettlementEntry{
		PositionDirection:      "Long",
		PriceDenom:             "USDC",
		AssetDenom:             "ATOM",
		Quantity:               sdk.NewDec(8).Quo(sdk.NewDec(3)),
		ExecutionCostOrProceed: sdk.MustNewDecFromStr("96.666666666666666667"),
		ExpectedCostOrProceed:  sdk.NewDec(100),
		Account:                "abc",
		OrderType:              "Market",
		OrderId:                2,
	})
	assert.Equal(t, *settlements[7], types.SettlementEntry{
		PositionDirection:      "Long",
		PriceDenom:             "USDC",
		AssetDenom:             "ATOM",
		Quantity:               sdk.NewDec(4).Quo(sdk.NewDec(3)),
		ExecutionCostOrProceed: sdk.MustNewDecFromStr("96.666666666666666667"),
		ExpectedCostOrProceed:  sdk.NewDec(100),
		Account:                "abc",
		OrderType:              "Market",
		OrderId:                2,
	})
}

func TestMatchMultipleMarketOrderFromMultipleLongBook(t *testing.T) {
	_, ctx := keepertest.DexKeeper(t)
	shortOrders := []types.Order{
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
	longBook := []types.OrderBook{
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
	dirtyPrices := exchange.NewDirtyPrices()
	settlements := []*types.SettlementEntry{}
	totalPrice, totalExecuted := exchange.MatchMarketOrders(
		ctx, shortOrders, longBook, TEST_PAIR(), types.PositionDirection_SHORT, &dirtyPrices, &settlements, &[]exchange.AccountOrderID{},
	)
	assert.Equal(t, totalPrice, sdk.NewDec(620))
	assert.Equal(t, totalExecuted, sdk.NewDec(6))
	assert.Equal(t, len(dirtyPrices.Get()), 2)
	assert.Equal(t, dirtyPrices.Has(sdk.NewDec(100)), true)
	assert.Equal(t, dirtyPrices.Has(sdk.NewDec(110)), true)
	assert.Equal(t, len(longBook), 2)
	assert.Equal(t, longBook[0].GetPrice(), sdk.NewDec(100))
	assert.Equal(t, *longBook[0].GetEntry(), types.OrderEntry{
		Price:    sdk.NewDec(100),
		Quantity: sdk.NewDec(2),
		Allocations: []*types.Allocation{{
			OrderId:  5,
			Account:  "abc",
			Quantity: sdk.NewDec(4).Quo(sdk.NewDec(3)),
		}, {
			OrderId:  6,
			Account:  "ghi",
			Quantity: sdk.NewDec(2).Quo(sdk.NewDec(3)),
		}},
		PriceDenom: "USDC",
		AssetDenom: "ATOM",
	})
	assert.Equal(t, longBook[1].GetPrice(), sdk.NewDec(110))
	assert.Equal(t, longBook[1].GetEntry().Price, sdk.NewDec(110))
	assert.Equal(t, longBook[1].GetEntry().Quantity.IsZero(), true)
	assert.Equal(t, len(settlements), 8)
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
	})
	assert.Equal(t, *settlements[2], types.SettlementEntry{
		PositionDirection:      "Long",
		PriceDenom:             "USDC",
		AssetDenom:             "ATOM",
		Quantity:               sdk.NewDec(8).Quo(sdk.NewDec(3)),
		ExecutionCostOrProceed: sdk.NewDec(100),
		ExpectedCostOrProceed:  sdk.NewDec(100),
		Account:                "abc",
		OrderType:              "Limit",
		OrderId:                5,
	})
	assert.Equal(t, *settlements[3], types.SettlementEntry{
		PositionDirection:      "Long",
		PriceDenom:             "USDC",
		AssetDenom:             "ATOM",
		Quantity:               sdk.NewDec(4).Quo(sdk.NewDec(3)),
		ExecutionCostOrProceed: sdk.NewDec(100),
		ExpectedCostOrProceed:  sdk.NewDec(100),
		Account:                "ghi",
		OrderType:              "Limit",
		OrderId:                6,
	})
	assert.Equal(t, *settlements[4], types.SettlementEntry{
		PositionDirection:      "Short",
		PriceDenom:             "USDC",
		AssetDenom:             "ATOM",
		Quantity:               sdk.NewDec(1),
		ExecutionCostOrProceed: sdk.MustNewDecFromStr("103.333333333333333333"),
		ExpectedCostOrProceed:  sdk.NewDec(96),
		Account:                "jkl",
		OrderType:              "Market",
		OrderId:                1,
	})
	assert.Equal(t, *settlements[5], types.SettlementEntry{
		PositionDirection:      "Short",
		PriceDenom:             "USDC",
		AssetDenom:             "ATOM",
		Quantity:               sdk.NewDec(1),
		ExecutionCostOrProceed: sdk.MustNewDecFromStr("103.333333333333333333"),
		ExpectedCostOrProceed:  sdk.NewDec(100),
		Account:                "abc",
		OrderType:              "Market",
		OrderId:                2,
	})
	assert.Equal(t, *settlements[6], types.SettlementEntry{
		PositionDirection:      "Short",
		PriceDenom:             "USDC",
		AssetDenom:             "ATOM",
		Quantity:               sdk.NewDec(8).Quo(sdk.NewDec(3)),
		ExecutionCostOrProceed: sdk.MustNewDecFromStr("103.333333333333333333"),
		ExpectedCostOrProceed:  sdk.NewDec(100),
		Account:                "abc",
		OrderType:              "Market",
		OrderId:                2,
	})
	assert.Equal(t, *settlements[7], types.SettlementEntry{
		PositionDirection:      "Short",
		PriceDenom:             "USDC",
		AssetDenom:             "ATOM",
		Quantity:               sdk.NewDec(4).Quo(sdk.NewDec(3)),
		ExecutionCostOrProceed: sdk.MustNewDecFromStr("103.333333333333333333"),
		ExpectedCostOrProceed:  sdk.NewDec(100),
		Account:                "abc",
		OrderType:              "Market",
		OrderId:                2,
	})
}
