package exchange_test

import (
	"testing"
	"time"

	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/sei-protocol/sei-chain/testutil/fuzzing"
	keepertest "github.com/sei-protocol/sei-chain/testutil/keeper"
	"github.com/sei-protocol/sei-chain/x/dex/exchange"
	"github.com/sei-protocol/sei-chain/x/dex/keeper"
	keeperutil "github.com/sei-protocol/sei-chain/x/dex/keeper/utils"
	"github.com/sei-protocol/sei-chain/x/dex/types"
	"github.com/stretchr/testify/require"
	"github.com/tendermint/tendermint/libs/log"
	tmproto "github.com/tendermint/tendermint/proto/tendermint/types"
)

var TestFuzzLimitCtx = sdk.NewContext(nil, tmproto.Header{}, false, log.NewNopLogger())
var TestFuzzLimitKeeper keeper.Keeper

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
	dexkeeper, ctx := keepertest.DexKeeper(t)
	ctx = ctx.WithBlockHeight(1).WithBlockTime(time.Now())
	buyEntries := fuzzing.GetOrderBookEntries(true, keepertest.TestPriceDenom, keepertest.TestAssetDenom, buyEntryWeights, buyAccountIndices, buyAllocationWeights)
	for _, entry := range buyEntries {
		dexkeeper.SetLongOrderBookEntry(ctx, keepertest.TestContract, entry)
	}
	sellEntries := fuzzing.GetOrderBookEntries(false, keepertest.TestPriceDenom, keepertest.TestAssetDenom, sellEntryWeights, sellAccountIndices, sellAllocationWeights)
	for _, entry := range sellEntries {
		dexkeeper.SetShortOrderBookEntry(ctx, keepertest.TestContract, entry)
	}
	buyOrders := fuzzing.GetPlacedOrders(types.PositionDirection_LONG, types.OrderType_LIMIT, keepertest.TestPair, buyPrices, buyQuantities)
	sellOrders := fuzzing.GetPlacedOrders(types.PositionDirection_SHORT, types.OrderType_LIMIT, keepertest.TestPair, sellPrices, sellQuantities)
	exchange.AddOutstandingLimitOrdersToOrderbook(ctx, dexkeeper, buyOrders, sellOrders)
	orderBook := keeperutil.PopulateOrderbook(ctx, dexkeeper, types.ContractAddress(keepertest.TestContract), types.Pair{PriceDenom: keepertest.TestPriceDenom, AssetDenom: keepertest.TestAssetDenom})
	require.NotPanics(t, func() { exchange.MatchLimitOrders(ctx, orderBook) })
}
