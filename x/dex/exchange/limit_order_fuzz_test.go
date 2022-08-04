package exchange_test

import (
	"testing"
	"time"

	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/sei-protocol/sei-chain/testutil/fuzzing"
	"github.com/sei-protocol/sei-chain/x/dex/exchange"
	"github.com/sei-protocol/sei-chain/x/dex/types"
	"github.com/stretchr/testify/require"

	keepertest "github.com/sei-protocol/sei-chain/testutil/keeper"
)

var TestFuzzLimitCtx = sdk.Context{}

func FuzzMatchLimitOrders(f *testing.F) {
	TestFuzzLimitCtx = TestFuzzLimitCtx.WithBlockHeight(1).WithBlockTime(time.Now())
	f.Fuzz(fuzzTargetMatchLimitOrders)
}

func fuzzTargetMatchLimitOrders(
	t *testing.T,
	buyPrices []byte,
	sellPrices []byte,
	buyQuantities []byte,
	sellQuantities []byte,
	buyEntryWeights []byte,
	sellEntryWeights []byte,
	buyAccountIndices []byte,
	sellAccountIndices []byte,
	buyAllocationWeights []byte,
	sellAllocationWeights []byte,
) {
	buyEntries := fuzzing.GetOrderBookEntries(true, keepertest.TestPriceDenom, keepertest.TestAssetDenom, buyEntryWeights, buyAccountIndices, buyAllocationWeights)
	sellEntries := fuzzing.GetOrderBookEntries(false, keepertest.TestPriceDenom, keepertest.TestAssetDenom, sellEntryWeights, sellAccountIndices, sellAllocationWeights)
	buyOrders := fuzzing.GetPlacedOrders(types.PositionDirection_LONG, types.OrderType_LIMIT, keepertest.TestPair, buyPrices, buyQuantities)
	sellOrders := fuzzing.GetPlacedOrders(types.PositionDirection_SHORT, types.OrderType_LIMIT, keepertest.TestPair, sellPrices, sellQuantities)
	orderBook := types.OrderBook{
		Longs:  &types.CachedSortedOrderBookEntries{Entries: buyEntries, DirtyEntries: map[string]types.OrderBookEntry{}},
		Shorts: &types.CachedSortedOrderBookEntries{Entries: sellEntries, DirtyEntries: map[string]types.OrderBookEntry{}},
	}
	require.NotPanics(t, func() { exchange.MatchLimitOrders(TestFuzzLimitCtx, buyOrders, sellOrders, &orderBook) })
}
