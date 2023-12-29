package contract_test

import (
	"testing"
	"time"

	"github.com/sei-protocol/sei-chain/utils/datastructures"
	dexutil "github.com/sei-protocol/sei-chain/x/dex/utils"

	sdk "github.com/cosmos/cosmos-sdk/types"
	keepertest "github.com/sei-protocol/sei-chain/testutil/keeper"
	"github.com/sei-protocol/sei-chain/x/dex/contract"
	keeperutil "github.com/sei-protocol/sei-chain/x/dex/keeper/utils"
	"github.com/sei-protocol/sei-chain/x/dex/types"
	"github.com/stretchr/testify/require"
)

func TEST_PAIR() types.Pair {
	return types.Pair{
		PriceDenom: "usdc",
		AssetDenom: "atom",
	}
}

const (
	TEST_ACCOUNT         = "test_account"
	TEST_CONTRACT        = "test"
	TestTimestamp uint64 = 10000
	TestHeight    uint64 = 1
)

func TestExecutePair(t *testing.T) {
	pair := types.Pair{
		PriceDenom: "USDC",
		AssetDenom: "ATOM",
	}
	dexkeeper, ctx := keepertest.DexKeeper(t)
	ctx = ctx.WithBlockHeight(int64(TestHeight)).WithBlockTime(time.Unix(int64(TestTimestamp), 0))
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
	for _, l := range longBook {
		dexkeeper.SetLongOrderBookEntry(ctx, keepertest.TestContract, l)
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
	for _, s := range shortBook {
		dexkeeper.SetShortOrderBookEntry(ctx, keepertest.TestContract, s)
	}
	orderbook := keeperutil.PopulateOrderbook(ctx, dexkeeper, types.ContractAddress(keepertest.TestContract), types.Pair{PriceDenom: "USDC", AssetDenom: "ATOM"})

	settlements := contract.ExecutePair(
		ctx,
		TEST_CONTRACT,
		pair,
		dexkeeper,
		orderbook,
	)
	require.Equal(t, len(settlements), 0)

	// add Market orders to the orderbook
	dexutil.GetMemState(ctx.Context()).GetBlockOrders(ctx, types.ContractAddress(TEST_CONTRACT), pair).Add(
		&types.Order{
			Id:                1,
			Account:           TEST_ACCOUNT,
			ContractAddr:      TEST_CONTRACT,
			Price:             sdk.MustNewDecFromStr("97"),
			Quantity:          sdk.MustNewDecFromStr("1"),
			PriceDenom:        pair.PriceDenom,
			AssetDenom:        pair.AssetDenom,
			OrderType:         types.OrderType_LIMIT,
			PositionDirection: types.PositionDirection_LONG,
			Data:              "{\"position_effect\":\"Open\",\"leverage\":\"1\"}",
		},
	)
	dexutil.GetMemState(ctx.Context()).GetBlockOrders(ctx, types.ContractAddress(TEST_CONTRACT), pair).Add(
		&types.Order{
			Id:                2,
			Account:           TEST_ACCOUNT,
			ContractAddr:      TEST_CONTRACT,
			Price:             sdk.MustNewDecFromStr("100"),
			Quantity:          sdk.MustNewDecFromStr("1"),
			PriceDenom:        pair.PriceDenom,
			AssetDenom:        pair.AssetDenom,
			OrderType:         types.OrderType_LIMIT,
			PositionDirection: types.PositionDirection_LONG,
			Data:              "{\"position_effect\":\"Open\",\"leverage\":\"1\"}",
		},
	)
	dexutil.GetMemState(ctx.Context()).GetBlockOrders(ctx, types.ContractAddress(TEST_CONTRACT), pair).Add(
		&types.Order{
			Id:                3,
			Account:           TEST_ACCOUNT,
			ContractAddr:      TEST_CONTRACT,
			Price:             sdk.MustNewDecFromStr("200"),
			Quantity:          sdk.MustNewDecFromStr("1"),
			PriceDenom:        pair.PriceDenom,
			AssetDenom:        pair.AssetDenom,
			OrderType:         types.OrderType_MARKET,
			PositionDirection: types.PositionDirection_LONG,
			Data:              "{\"position_effect\":\"Open\",\"leverage\":\"1\"}",
		},
	)

	settlements = contract.ExecutePair(
		ctx,
		TEST_CONTRACT,
		pair,
		dexkeeper,
		orderbook,
	)

	require.Equal(t, 2, len(settlements))
	require.Equal(t, uint64(7), settlements[0].OrderId)
	require.Equal(t, uint64(3), settlements[1].OrderId)

	// get match results
	matches, cancels := contract.GetMatchResults(
		ctx,
		TEST_CONTRACT,
		pair,
	)
	require.Equal(t, 3, len(matches))
	require.Equal(t, 0, len(cancels))
}

func TestExecutePairInParallel(t *testing.T) {
	pair := types.Pair{
		PriceDenom: "USDC",
		AssetDenom: "ATOM",
	}
	dexkeeper, ctx := keepertest.DexKeeper(t)
	ctx = ctx.WithBlockHeight(int64(TestHeight)).WithBlockTime(time.Unix(int64(TestTimestamp), 0))
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
	for _, l := range longBook {
		dexkeeper.SetLongOrderBookEntry(ctx, TEST_CONTRACT, l)
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
	for _, s := range shortBook {
		dexkeeper.SetShortOrderBookEntry(ctx, TEST_CONTRACT, s)
	}
	orderbook := keeperutil.PopulateOrderbook(ctx, dexkeeper, types.ContractAddress(TEST_CONTRACT), types.Pair{PriceDenom: "USDC", AssetDenom: "ATOM"})

	// execute in parallel simple path
	orderbooks := datastructures.NewTypedSyncMap[types.PairString, *types.OrderBook]()
	orderbooks.Store(types.GetPairString(&pair), orderbook)
	settlements := contract.ExecutePairsInParallel(
		ctx,
		TEST_CONTRACT,
		dexkeeper,
		[]types.Pair{pair},
		orderbooks,
	)

	require.Equal(t, len(settlements), 0)

	// add Market orders to the orderbook
	dexutil.GetMemState(ctx.Context()).GetBlockOrders(ctx, types.ContractAddress(TEST_CONTRACT), pair).Add(
		&types.Order{
			Id:                1,
			Account:           TEST_ACCOUNT,
			ContractAddr:      TEST_CONTRACT,
			Price:             sdk.MustNewDecFromStr("97"),
			Quantity:          sdk.MustNewDecFromStr("1"),
			PriceDenom:        pair.PriceDenom,
			AssetDenom:        pair.AssetDenom,
			OrderType:         types.OrderType_LIMIT,
			PositionDirection: types.PositionDirection_LONG,
			Data:              "{\"position_effect\":\"Open\",\"leverage\":\"1\"}",
		},
	)
	dexutil.GetMemState(ctx.Context()).GetBlockOrders(ctx, types.ContractAddress(TEST_CONTRACT), pair).Add(
		&types.Order{
			Id:                2,
			Account:           TEST_ACCOUNT,
			ContractAddr:      TEST_CONTRACT,
			Price:             sdk.MustNewDecFromStr("100"),
			Quantity:          sdk.MustNewDecFromStr("1"),
			PriceDenom:        pair.PriceDenom,
			AssetDenom:        pair.AssetDenom,
			OrderType:         types.OrderType_LIMIT,
			PositionDirection: types.PositionDirection_LONG,
			Data:              "{\"position_effect\":\"Open\",\"leverage\":\"1\"}",
		},
	)
	dexutil.GetMemState(ctx.Context()).GetBlockOrders(ctx, types.ContractAddress(TEST_CONTRACT), pair).Add(
		&types.Order{
			Id:                3,
			Account:           TEST_ACCOUNT,
			ContractAddr:      TEST_CONTRACT,
			Price:             sdk.MustNewDecFromStr("200"),
			Quantity:          sdk.MustNewDecFromStr("1"),
			PriceDenom:        pair.PriceDenom,
			AssetDenom:        pair.AssetDenom,
			OrderType:         types.OrderType_MARKET,
			PositionDirection: types.PositionDirection_LONG,
			Data:              "{\"position_effect\":\"Open\",\"leverage\":\"1\"}",
		},
	)
	dexutil.GetMemState(ctx.Context()).GetBlockOrders(ctx, types.ContractAddress(TEST_CONTRACT), pair).Add(
		&types.Order{
			Id:                11,
			Account:           TEST_ACCOUNT,
			ContractAddr:      TEST_CONTRACT,
			Price:             sdk.MustNewDecFromStr("20"),
			Quantity:          sdk.MustNewDecFromStr("1"),
			PriceDenom:        pair.PriceDenom,
			AssetDenom:        pair.AssetDenom,
			OrderType:         types.OrderType_MARKET,
			PositionDirection: types.PositionDirection_LONG,
			Data:              "{\"position_effect\":\"Open\",\"leverage\":\"1\"}",
		},
	)

	settlements = contract.ExecutePairsInParallel(
		ctx,
		TEST_CONTRACT,
		dexkeeper,
		[]types.Pair{pair},
		orderbooks,
	)

	require.Equal(t, 2, len(settlements))
	require.Equal(t, uint64(7), settlements[0].OrderId)
	require.Equal(t, uint64(3), settlements[1].OrderId)
}

func TestGetOrderIDToSettledQuantities(t *testing.T) {
	settlements := []*types.SettlementEntry{
		{
			OrderId:  1,
			Quantity: sdk.MustNewDecFromStr("100"),
		},
		{
			OrderId:  2,
			Quantity: sdk.MustNewDecFromStr("200"),
		},
	}

	idMapping := contract.GetOrderIDToSettledQuantities(settlements)

	require.Equal(t, 2, len(idMapping))
	require.Equal(t, sdk.MustNewDecFromStr("100"), idMapping[1])
	require.Equal(t, sdk.MustNewDecFromStr("200"), idMapping[2])
}

func TestEmitSettlementMetrics(t *testing.T) {
	settlements := []*types.SettlementEntry{
		{
			OrderId:  1,
			Quantity: sdk.MustNewDecFromStr("100"),
		},
		{
			OrderId:  2,
			Quantity: sdk.MustNewDecFromStr("200"),
		},
	}

	require.NotPanics(t, func() { contract.EmitSettlementMetrics(settlements) })
}
