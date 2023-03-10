package exchange_test

import (
	"testing"
	"time"

	"github.com/cosmos/cosmos-sdk/store"
	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/sei-protocol/sei-chain/testutil/fuzzing"
	keepertest "github.com/sei-protocol/sei-chain/testutil/keeper"
	dex "github.com/sei-protocol/sei-chain/x/dex/cache"
	"github.com/sei-protocol/sei-chain/x/dex/exchange"
	"github.com/sei-protocol/sei-chain/x/dex/types"
	"github.com/stretchr/testify/require"
	"github.com/tendermint/tendermint/libs/log"
	tmproto "github.com/tendermint/tendermint/proto/tendermint/types"
	tmdb "github.com/tendermint/tm-db"
)

var db = tmdb.NewMemDB()
var stateStore = store.NewCommitMultiStore(db)
var ctx = sdk.NewContext(stateStore, tmproto.Header{}, false, log.NewNopLogger())
var TestFuzzSettleCtx = sdk.NewContext(stateStore, tmproto.Header{}, false, log.NewNopLogger())

func FuzzSettleMarketOrder(f *testing.F) {
	TestFuzzSettleCtx = TestFuzzSettleCtx.WithBlockHeight(1).WithBlockTime(time.Now())
	f.Fuzz(fuzzTargetMatchMarketOrders)
}

func fuzzTargetSettle(
	t *testing.T,
	long bool,
	prices []byte,
	quantities []byte,
	entryWeights []byte,
	accountIndices []byte,
	allocationWeights []byte,
	priceI int64,
	priceIsNil bool,
	quantityI int64,
	quantityIsNil bool,
) {
	entries := fuzzing.GetOrderBookEntries(!long, keepertest.TestPriceDenom, keepertest.TestAssetDenom, entryWeights, accountIndices, allocationWeights)
	var direction types.PositionDirection
	if long {
		direction = types.PositionDirection_LONG
	} else {
		direction = types.PositionDirection_SHORT
	}
	orders := fuzzing.GetPlacedOrders(direction, types.OrderType_MARKET, keepertest.TestPair, prices, quantities)

	price := fuzzing.FuzzDec(priceI, priceIsNil)
	quantity := fuzzing.FuzzDec(quantityI, quantityIsNil)

	if len(entries) > len(orders) {
		entries = entries[:len(orders)]
	} else {
		orders = orders[:len(entries)]
	}

	for i, entry := range entries {
		require.NotPanics(t, func() {
			exchange.Settle(TestFuzzSettleCtx, orders[i], quantity, entry, price, &dex.BlockOrders{})
		})
	}
}

func FuzzSettleLimitOrder(f *testing.F) {
	TestFuzzSettleCtx = TestFuzzSettleCtx.WithBlockHeight(1).WithBlockTime(time.Now())
	f.Fuzz(fuzzTargetMatchMarketOrders)
}

func fuzzTargetSettleFromBook(
	t *testing.T,
	buyEntryWeights []byte,
	sellEntryWeights []byte,
	buyAccountIndices []byte,
	sellAccountIndices []byte,
	buyAllocationWeights []byte,
	sellAllocationWeights []byte,
	quantityI int64,
	quantityIsNil bool,
) {
	buyEntries := fuzzing.GetOrderBookEntries(true, keepertest.TestPriceDenom, keepertest.TestAssetDenom, buyEntryWeights, buyAccountIndices, buyAllocationWeights)
	sellEntries := fuzzing.GetOrderBookEntries(false, keepertest.TestPriceDenom, keepertest.TestAssetDenom, sellEntryWeights, sellAccountIndices, sellAllocationWeights)

	quantity := fuzzing.FuzzDec(quantityI, quantityIsNil)

	if len(buyEntries) > len(sellEntries) {
		buyEntries = buyEntries[:len(sellEntries)]
	} else {
		sellEntries = sellEntries[:len(buyEntries)]
	}

	for i, longEntry := range buyEntries {
		require.NotPanics(t, func() {
			exchange.SettleFromBook(TestFuzzSettleCtx, longEntry, sellEntries[i], quantity)
		})
	}
}
