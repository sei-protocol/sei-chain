package exchange_test

import (
	"testing"
	"time"

	sdk "github.com/cosmos/cosmos-sdk/types"
	dex "github.com/sei-protocol/sei-chain/x/dex/cache"
	"github.com/sei-protocol/sei-chain/x/dex/exchange"
	keeperutil "github.com/sei-protocol/sei-chain/x/dex/keeper/utils"
	"github.com/sei-protocol/sei-chain/x/dex/types"
	dexutils "github.com/sei-protocol/sei-chain/x/dex/utils"
	"github.com/stretchr/testify/assert"

	keepertest "github.com/sei-protocol/sei-chain/testutil/keeper"
)

const (
	TestTimestamp uint64 = 10000
	TestHeight    uint64 = 1
)

func TestMatchFoKMarketOrderFromShortBookNotEnoughLiquidity(t *testing.T) {
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
					Quantity: sdk.NewDec(4),
				}},
				PriceDenom: "USDC",
				AssetDenom: "ATOM",
			},
		},
	}
	blockOrders := dexutils.GetMemState(ctx.Context()).GetBlockOrders(ctx, "testAccount", types.Pair{PriceDenom: "USDC", AssetDenom: "ATOM"})
	blockOrders.Add(&types.Order{
		Id:                1,
		Account:           "abc",
		ContractAddr:      "test",
		Price:             sdk.NewDec(100),
		Quantity:          sdk.NewDec(5),
		PriceDenom:        "USDC",
		AssetDenom:        "ATOM",
		OrderType:         types.OrderType_FOKMARKET,
		PositionDirection: types.PositionDirection_LONG,
		Data:              "{\"position_effect\":\"Open\",\"leverage\":\"1\"}",
	})

	for _, e := range shortBook {
		dexkeeper.SetShortOrderBookEntry(ctx, "test", e)
	}
	orderbook := keeperutil.PopulateOrderbook(ctx, dexkeeper, types.ContractAddress("test"), types.Pair{PriceDenom: "USDC", AssetDenom: "ATOM"})
	entries := orderbook.Shorts
	outcome := exchange.MatchMarketOrders(
		ctx, longOrders, entries, types.PositionDirection_LONG, blockOrders,
	)
	totalPrice := outcome.TotalNotional
	totalExecuted := outcome.TotalQuantity
	settlements := outcome.Settlements

	shortBook = dexkeeper.GetAllShortBookForPair(ctx, "test", "USDC", "ATOM")
	assert.Equal(t, totalPrice, sdk.ZeroDec())
	assert.Equal(t, totalExecuted, sdk.ZeroDec())
	assert.Equal(t, len(shortBook), 1)
	assert.Equal(t, shortBook[0].GetPrice(), sdk.NewDec(100))
	assert.Equal(t, shortBook[0].GetOrderEntry().Quantity, sdk.NewDec(4))
	assert.Equal(t, len(settlements), 0)
	assert.Equal(t, blockOrders.Get()[0].Quantity, sdk.NewDec(5))
	assert.Equal(t, blockOrders.Get()[0].Status, types.OrderStatus_PLACED)
}

func TestMatchFoKMarketOrderFromShortBookHappyPath(t *testing.T) {
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
	for _, e := range shortBook {
		dexkeeper.SetShortOrderBookEntry(ctx, "test", e)
	}
	orderbook := keeperutil.PopulateOrderbook(ctx, dexkeeper, types.ContractAddress("test"), types.Pair{PriceDenom: "USDC", AssetDenom: "ATOM"})
	entries := orderbook.Shorts
	blockOrders := dexutils.GetMemState(ctx.Context()).GetBlockOrders(ctx, "testAccount", types.Pair{PriceDenom: "USDC", AssetDenom: "ATOM"})
	blockOrders.Add(&types.Order{
		Id:                1,
		Account:           "abc",
		ContractAddr:      "test",
		Price:             sdk.NewDec(100),
		Quantity:          sdk.NewDec(5),
		PriceDenom:        "USDC",
		AssetDenom:        "ATOM",
		OrderType:         types.OrderType_FOKMARKET,
		PositionDirection: types.PositionDirection_LONG,
		Data:              "{\"position_effect\":\"Open\",\"leverage\":\"1\"}",
	})
	outcome := exchange.MatchMarketOrders(
		ctx, longOrders, entries, types.PositionDirection_LONG, blockOrders,
	)
	totalPrice := outcome.TotalNotional
	totalExecuted := outcome.TotalQuantity
	settlements := outcome.Settlements

	shortBook = dexkeeper.GetAllShortBookForPair(ctx, "test", "USDC", "ATOM")
	assert.Equal(t, totalPrice, sdk.NewDec(500))
	assert.Equal(t, totalExecuted, sdk.NewDec(5))
	assert.Equal(t, len(shortBook), 0)
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
	assert.Equal(t, blockOrders.Get()[0].Quantity, sdk.ZeroDec())
	assert.Equal(t, blockOrders.Get()[0].Status, types.OrderStatus_FULFILLED)
}

func TestMatchByValueFOKMarketOrderFromShortBookNotEnoughLiquidity(t *testing.T) {
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
			OrderType:         types.OrderType_FOKMARKETBYVALUE,
			Nominal:           sdk.NewDec(500),
		},
	}
	shortBook := []types.OrderBookEntry{
		&types.ShortBook{
			Price: sdk.NewDec(90),
			Entry: &types.OrderEntry{
				Price:    sdk.NewDec(90),
				Quantity: sdk.NewDec(5),
				Allocations: []*types.Allocation{{
					OrderId:  4,
					Account:  "def",
					Quantity: sdk.NewDec(5),
				}},
				PriceDenom: "USDC",
				AssetDenom: "ATOM",
			},
		},
		&types.ShortBook{
			Price: sdk.NewDec(100),
			Entry: &types.OrderEntry{
				Price:    sdk.NewDec(100),
				Quantity: sdk.MustNewDecFromStr("0.4"),
				Allocations: []*types.Allocation{{
					OrderId:  5,
					Account:  "def",
					Quantity: sdk.MustNewDecFromStr("0.4"),
				}},
				PriceDenom: "USDC",
				AssetDenom: "ATOM",
			},
		},
	}
	blockOrders := dexutils.GetMemState(ctx.Context()).GetBlockOrders(ctx, "testAccount", types.Pair{PriceDenom: "USDC", AssetDenom: "ATOM"})
	blockOrders.Add(longOrders[0])
	for _, e := range shortBook {
		dexkeeper.SetShortOrderBookEntry(ctx, "test", e)
	}
	orderbook := keeperutil.PopulateOrderbook(ctx, dexkeeper, types.ContractAddress("test"), types.Pair{PriceDenom: "USDC", AssetDenom: "ATOM"})
	entries := orderbook.Shorts
	outcome := exchange.MatchMarketOrders(
		ctx, longOrders, entries, types.PositionDirection_LONG, &dex.BlockOrders{},
	)
	totalPrice := outcome.TotalNotional
	totalExecuted := outcome.TotalQuantity
	settlements := outcome.Settlements
	shortBook = dexkeeper.GetAllShortBookForPair(ctx, "test", "USDC", "ATOM")
	assert.Equal(t, totalPrice, sdk.ZeroDec())
	assert.Equal(t, totalExecuted, sdk.ZeroDec())
	assert.Equal(t, len(shortBook), 2)
	assert.Equal(t, shortBook[0].GetPrice(), sdk.NewDec(90))
	assert.Equal(t, shortBook[0].GetOrderEntry().Price, sdk.NewDec(90))
	assert.Equal(t, shortBook[0].GetOrderEntry().Quantity, sdk.NewDec(5))
	assert.Equal(t, len(settlements), 0)
	assert.Equal(t, blockOrders.Get()[0].Quantity, sdk.NewDec(5))
	assert.Equal(t, blockOrders.Get()[0].Status, types.OrderStatus_PLACED)
}

func TestMatchByValueFOKMarketOrderFromShortBookHappyPath(t *testing.T) {
	dexkeeper, ctx := keepertest.DexKeeper(t)
	ctx = ctx.WithBlockHeight(int64(TestHeight)).WithBlockTime(time.Unix(int64(TestTimestamp), 0))
	longOrders := []*types.Order{
		{
			Id:                1,
			Price:             sdk.NewDec(100),
			Quantity:          sdk.NewDec(6),
			Account:           "abc",
			PositionDirection: types.PositionDirection_LONG,
			ContractAddr:      "test",
			PriceDenom:        "USDC",
			AssetDenom:        "ATOM",
			OrderType:         types.OrderType_FOKMARKETBYVALUE,
			Nominal:           sdk.NewDec(500),
		},
	}
	shortBook := []types.OrderBookEntry{
		&types.ShortBook{
			Price: sdk.NewDec(90),
			Entry: &types.OrderEntry{
				Price:    sdk.NewDec(90),
				Quantity: sdk.NewDec(5),
				Allocations: []*types.Allocation{{
					OrderId:  4,
					Account:  "def",
					Quantity: sdk.NewDec(5),
				}},
				PriceDenom: "USDC",
				AssetDenom: "ATOM",
			},
		},
		&types.ShortBook{
			Price: sdk.NewDec(100),
			Entry: &types.OrderEntry{
				Price:    sdk.NewDec(100),
				Quantity: sdk.MustNewDecFromStr("0.6"),
				Allocations: []*types.Allocation{{
					OrderId:  5,
					Account:  "def",
					Quantity: sdk.MustNewDecFromStr("0.6"),
				}},
				PriceDenom: "USDC",
				AssetDenom: "ATOM",
			},
		},
	}
	blockOrders := dexutils.GetMemState(ctx.Context()).GetBlockOrders(ctx, "testAccount", types.Pair{PriceDenom: "USDC", AssetDenom: "ATOM"})
	blockOrders.Add(longOrders[0])
	for _, e := range shortBook {
		dexkeeper.SetShortOrderBookEntry(ctx, "test", e)
	}
	orderbook := keeperutil.PopulateOrderbook(ctx, dexkeeper, types.ContractAddress("test"), types.Pair{PriceDenom: "USDC", AssetDenom: "ATOM"})
	entries := orderbook.Shorts
	outcome := exchange.MatchMarketOrders(
		ctx, longOrders, entries, types.PositionDirection_LONG, blockOrders,
	)
	totalPrice := outcome.TotalNotional
	totalExecuted := outcome.TotalQuantity
	settlements := outcome.Settlements
	shortBook = dexkeeper.GetAllShortBookForPair(ctx, "test", "USDC", "ATOM")
	assert.Equal(t, totalPrice, sdk.NewDec(500))
	assert.Equal(t, totalExecuted, sdk.MustNewDecFromStr("5.5"))
	assert.Equal(t, len(shortBook), 1)
	assert.Equal(t, len(settlements), 3)
	assert.Equal(t, *settlements[0], types.SettlementEntry{
		PositionDirection:      "Short",
		PriceDenom:             "USDC",
		AssetDenom:             "ATOM",
		Quantity:               sdk.NewDec(5),
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
		Quantity:               sdk.MustNewDecFromStr("0.5"),
		ExecutionCostOrProceed: sdk.NewDec(100),
		ExpectedCostOrProceed:  sdk.NewDec(100),
		Account:                "def",
		OrderType:              "Limit",
		OrderId:                5,
		Timestamp:              TestTimestamp,
		Height:                 TestHeight,
	})
	assert.Equal(t, blockOrders.Get()[0].Quantity, sdk.MustNewDecFromStr("0.5"))
	assert.Equal(t, blockOrders.Get()[0].Status, types.OrderStatus_FULFILLED)
}

func TestMatchSingleMarketOrderFromShortBook(t *testing.T) {
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
	blockOrders := dexutils.GetMemState(ctx.Context()).GetBlockOrders(ctx, "testAccount", types.Pair{PriceDenom: "USDC", AssetDenom: "ATOM"})
	blockOrders.Add(longOrders[0])
	for _, e := range shortBook {
		dexkeeper.SetShortOrderBookEntry(ctx, "test", e)
	}
	orderbook := keeperutil.PopulateOrderbook(ctx, dexkeeper, types.ContractAddress("test"), types.Pair{PriceDenom: "USDC", AssetDenom: "ATOM"})
	entries := orderbook.Shorts
	outcome := exchange.MatchMarketOrders(
		ctx, longOrders, entries, types.PositionDirection_LONG, blockOrders,
	)
	totalPrice := outcome.TotalNotional
	totalExecuted := outcome.TotalQuantity
	settlements := outcome.Settlements
	shortBook = dexkeeper.GetAllShortBookForPair(ctx, "test", "USDC", "ATOM")
	assert.Equal(t, totalPrice, sdk.NewDec(500))
	assert.Equal(t, totalExecuted, sdk.NewDec(5))
	assert.Equal(t, len(shortBook), 0)
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
	assert.Equal(t, blockOrders.Get()[0].Quantity, sdk.ZeroDec())
	assert.Equal(t, blockOrders.Get()[0].Status, types.OrderStatus_FULFILLED)
}

func TestMatchSingleMarketOrderFromLongBook(t *testing.T) {
	dexkeeper, ctx := keepertest.DexKeeper(t)
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
	blockOrders := dexutils.GetMemState(ctx.Context()).GetBlockOrders(ctx, "testAccount", types.Pair{PriceDenom: "USDC", AssetDenom: "ATOM"})
	blockOrders.Add(shortOrders[0])
	for _, e := range longBook {
		dexkeeper.SetLongOrderBookEntry(ctx, "test", e)
	}
	orderbook := keeperutil.PopulateOrderbook(ctx, dexkeeper, types.ContractAddress("test"), types.Pair{PriceDenom: "USDC", AssetDenom: "ATOM"})
	entries := orderbook.Longs
	outcome := exchange.MatchMarketOrders(
		ctx, shortOrders, entries, types.PositionDirection_SHORT, blockOrders,
	)
	totalPrice := outcome.TotalNotional
	totalExecuted := outcome.TotalQuantity
	settlements := outcome.Settlements
	longBook = dexkeeper.GetAllLongBookForPair(ctx, "test", "USDC", "ATOM")
	assert.Equal(t, totalPrice, sdk.NewDec(500))
	assert.Equal(t, totalExecuted, sdk.NewDec(5))
	assert.Equal(t, len(longBook), 0)
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
	assert.Equal(t, blockOrders.Get()[0].Quantity, sdk.ZeroDec())
	assert.Equal(t, blockOrders.Get()[0].Status, types.OrderStatus_FULFILLED)
}

func TestMatchSingleMarketOrderFromMultipleShortBook(t *testing.T) {
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
	blockOrders := dexutils.GetMemState(ctx.Context()).GetBlockOrders(ctx, "testAccount", types.Pair{PriceDenom: "USDC", AssetDenom: "ATOM"})
	blockOrders.Add(longOrders[0])
	for _, e := range shortBook {
		dexkeeper.SetShortOrderBookEntry(ctx, "test", e)
	}
	orderbook := keeperutil.PopulateOrderbook(ctx, dexkeeper, types.ContractAddress("test"), types.Pair{PriceDenom: "USDC", AssetDenom: "ATOM"})
	entries := orderbook.Shorts
	outcome := exchange.MatchMarketOrders(
		ctx, longOrders, entries, types.PositionDirection_LONG, blockOrders,
	)
	totalPrice := outcome.TotalNotional
	totalExecuted := outcome.TotalQuantity
	settlements := outcome.Settlements
	shortBook = dexkeeper.GetAllShortBookForPair(ctx, "test", "USDC", "ATOM")
	assert.Equal(t, totalPrice, sdk.NewDec(480))
	assert.Equal(t, totalExecuted, sdk.NewDec(5))
	assert.Equal(t, len(shortBook), 1)
	assert.Equal(t, shortBook[0].GetPrice(), sdk.NewDec(100))
	assert.Equal(t, *shortBook[0].GetOrderEntry(), types.OrderEntry{
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
	assert.Equal(t, blockOrders.Get()[0].Quantity, sdk.ZeroDec())
	assert.Equal(t, blockOrders.Get()[0].Status, types.OrderStatus_FULFILLED)
}

func TestMatchSingleMarketOrderFromMultipleLongBook(t *testing.T) {
	dexkeeper, ctx := keepertest.DexKeeper(t)
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
	blockOrders := dexutils.GetMemState(ctx.Context()).GetBlockOrders(ctx, "testAccount", types.Pair{PriceDenom: "USDC", AssetDenom: "ATOM"})
	blockOrders.Add(shortOrders[0])
	for _, e := range longBook {
		dexkeeper.SetLongOrderBookEntry(ctx, "test", e)
	}
	orderbook := keeperutil.PopulateOrderbook(ctx, dexkeeper, types.ContractAddress("test"), types.Pair{PriceDenom: "USDC", AssetDenom: "ATOM"})
	entries := orderbook.Longs
	outcome := exchange.MatchMarketOrders(
		ctx, shortOrders, entries, types.PositionDirection_SHORT, blockOrders,
	)
	totalPrice := outcome.TotalNotional
	totalExecuted := outcome.TotalQuantity
	settlements := outcome.Settlements
	longBook = dexkeeper.GetAllLongBookForPair(ctx, "test", "USDC", "ATOM")
	assert.Equal(t, totalPrice, sdk.NewDec(520))
	assert.Equal(t, totalExecuted, sdk.NewDec(5))
	assert.Equal(t, len(longBook), 1)
	assert.Equal(t, longBook[0].GetPrice(), sdk.NewDec(100))
	assert.Equal(t, *longBook[0].GetOrderEntry(), types.OrderEntry{
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
	assert.Equal(t, blockOrders.Get()[0].Quantity, sdk.ZeroDec())
	assert.Equal(t, blockOrders.Get()[0].Status, types.OrderStatus_FULFILLED)
}

func TestMatchMultipleMarketOrderFromMultipleShortBook(t *testing.T) {
	dexkeeper, ctx := keepertest.DexKeeper(t)
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
	blockOrders := dexutils.GetMemState(ctx.Context()).GetBlockOrders(ctx, "testAccount", types.Pair{PriceDenom: "USDC", AssetDenom: "ATOM"})
	blockOrders.Add(longOrders[0])
	blockOrders.Add(longOrders[1])
	blockOrders.Add(longOrders[2])
	for _, e := range shortBook {
		dexkeeper.SetShortOrderBookEntry(ctx, "test", e)
	}
	orderbook := keeperutil.PopulateOrderbook(ctx, dexkeeper, types.ContractAddress("test"), types.Pair{PriceDenom: "USDC", AssetDenom: "ATOM"})
	entries := orderbook.Shorts
	outcome := exchange.MatchMarketOrders(
		ctx, longOrders, entries, types.PositionDirection_LONG, blockOrders,
	)
	totalPrice := outcome.TotalNotional
	totalExecuted := outcome.TotalQuantity
	settlements := outcome.Settlements

	shortBook = dexkeeper.GetAllShortBookForPair(ctx, "test", "USDC", "ATOM")
	assert.Equal(t, totalPrice, sdk.NewDec(580))
	assert.Equal(t, totalExecuted, sdk.NewDec(6))
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
	assert.Equal(t, blockOrders.Get()[0].Quantity, sdk.ZeroDec())
	assert.Equal(t, blockOrders.Get()[0].Status, types.OrderStatus_FULFILLED)
	assert.Equal(t, blockOrders.Get()[1].Quantity, sdk.ZeroDec())
	assert.Equal(t, blockOrders.Get()[1].Status, types.OrderStatus_FULFILLED)
	assert.Equal(t, blockOrders.Get()[2].Quantity, sdk.NewDec(2))
	assert.Equal(t, blockOrders.Get()[2].Status, types.OrderStatus_PLACED)
}

func TestMatchMultipleMarketOrderFromMultipleLongBook(t *testing.T) {
	dexkeeper, ctx := keepertest.DexKeeper(t)
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
	blockOrders := dexutils.GetMemState(ctx.Context()).GetBlockOrders(ctx, "testAccount", types.Pair{PriceDenom: "USDC", AssetDenom: "ATOM"})
	blockOrders.Add(shortOrders[0])
	blockOrders.Add(shortOrders[1])
	blockOrders.Add(shortOrders[2])
	for _, e := range longBook {
		dexkeeper.SetLongOrderBookEntry(ctx, "test", e)
	}
	orderbook := keeperutil.PopulateOrderbook(ctx, dexkeeper, types.ContractAddress("test"), types.Pair{PriceDenom: "USDC", AssetDenom: "ATOM"})
	entries := orderbook.Longs
	outcome := exchange.MatchMarketOrders(
		ctx, shortOrders, entries, types.PositionDirection_SHORT, blockOrders,
	)
	totalPrice := outcome.TotalNotional
	totalExecuted := outcome.TotalQuantity
	settlements := outcome.Settlements
	minPrice := outcome.MinPrice
	maxPrice := outcome.MaxPrice

	longBook = dexkeeper.GetAllLongBookForPair(ctx, "test", "USDC", "ATOM")
	assert.Equal(t, totalPrice, sdk.NewDec(620))
	assert.Equal(t, totalExecuted, sdk.NewDec(6))
	assert.Equal(t, minPrice, sdk.MustNewDecFromStr("103.333333333333333333"))
	assert.Equal(t, maxPrice, sdk.MustNewDecFromStr("103.333333333333333333"))
	assert.Equal(t, len(longBook), 1)
	assert.Equal(t, longBook[0].GetPrice(), sdk.NewDec(100))
	assert.Equal(t, *longBook[0].GetOrderEntry(), types.OrderEntry{
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
	assert.Equal(t, blockOrders.Get()[0].Quantity, sdk.ZeroDec())
	assert.Equal(t, blockOrders.Get()[0].Status, types.OrderStatus_FULFILLED)
	assert.Equal(t, blockOrders.Get()[1].Quantity, sdk.ZeroDec())
	assert.Equal(t, blockOrders.Get()[1].Status, types.OrderStatus_FULFILLED)
	assert.Equal(t, blockOrders.Get()[2].Quantity, sdk.NewDec(2))
	assert.Equal(t, blockOrders.Get()[2].Status, types.OrderStatus_PLACED)
}
