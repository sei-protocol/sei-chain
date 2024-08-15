package exchange_test

import (
	"sort"
	"testing"
	"time"

	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/sei-protocol/sei-chain/testutil/fuzzing"
	keepertest "github.com/sei-protocol/sei-chain/testutil/keeper"
	"github.com/sei-protocol/sei-chain/x/dex/exchange"
	keeperutil "github.com/sei-protocol/sei-chain/x/dex/keeper/utils"
	"github.com/sei-protocol/sei-chain/x/dex/types"
	dexutils "github.com/sei-protocol/sei-chain/x/dex/utils"
	"github.com/stretchr/testify/require"
	"github.com/tendermint/tendermint/libs/log"
	tmproto "github.com/tendermint/tendermint/proto/tendermint/types"
)

var TestFuzzMarketCtx = sdk.NewContext(nil, tmproto.Header{}, false, log.NewNopLogger())

func FuzzMatchMarketOrders(f *testing.F) {
	f.Fuzz(fuzzTargetMatchMarketOrders)
}

func fuzzTargetMatchMarketOrders(
	t *testing.T,
	takerLong bool,
	orderSorted bool,
	orderbookSorted bool,
	prices []byte,
	quantities []byte,
	entryWeights []byte,
	accountIndices []byte,
	allocationWeights []byte,
) {
	dexkeeper, TestFuzzMarketCtx := keepertest.DexKeeper(t)
	TestFuzzMarketCtx = TestFuzzMarketCtx.WithBlockHeight(1).WithBlockTime(time.Now())
	blockOrders := dexutils.GetMemState(TestFuzzMarketCtx.Context()).GetBlockOrders(TestFuzzMarketCtx, "testAccount", types.Pair{PriceDenom: "USDC", AssetDenom: "ATOM"})
	entries := fuzzing.GetOrderBookEntries(!takerLong, keepertest.TestPriceDenom, keepertest.TestAssetDenom, entryWeights, accountIndices, allocationWeights)
	for _, entry := range entries {
		if takerLong {
			dexkeeper.SetShortOrderBookEntry(TestFuzzMarketCtx, keepertest.TestContract, entry)
		} else {
			dexkeeper.SetLongOrderBookEntry(TestFuzzMarketCtx, keepertest.TestContract, entry)
		}
	}
	var direction types.PositionDirection
	if takerLong {
		direction = types.PositionDirection_LONG
	} else {
		direction = types.PositionDirection_SHORT
	}
	orders := fuzzing.GetPlacedOrders(direction, types.OrderType_MARKET, keepertest.TestPair, prices, quantities)
	for _, order := range orders {
		blockOrders.Add(order)
	}

	if orderSorted {
		sort.Slice(orders, func(i, j int) bool {
			// a price of 0 indicates that there is no worst price for the order, so it should
			// always be ranked at the top.
			if orders[i].Price.IsZero() {
				return true
			} else if orders[j].Price.IsZero() {
				return false
			}
			switch direction {
			case types.PositionDirection_LONG:
				return orders[i].Price.GT(orders[j].Price)
			case types.PositionDirection_SHORT:
				return orders[i].Price.LT(orders[j].Price)
			default:
				panic("Unknown direction")
			}
		})
	}
	if orderbookSorted {
		sort.Slice(entries, func(i, j int) bool {
			return entries[i].GetPrice().LT(entries[j].GetPrice())
		})
	}

	require.NotPanics(t, func() {
		orderbook := keeperutil.PopulateOrderbook(TestFuzzMarketCtx, dexkeeper, types.ContractAddress(keepertest.TestContract), types.Pair{PriceDenom: keepertest.TestPriceDenom, AssetDenom: keepertest.TestAssetDenom})
		book := orderbook.Longs
		if takerLong {
			book = orderbook.Shorts
		}
		exchange.MatchMarketOrders(TestFuzzMarketCtx, orders, book, direction, blockOrders)
	})
}
