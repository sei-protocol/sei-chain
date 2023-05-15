package exchange_test

import (
	"testing"
	"time"

	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/sei-protocol/sei-chain/testutil/fuzzing"
	keepertest "github.com/sei-protocol/sei-chain/testutil/keeper"
	"github.com/sei-protocol/sei-chain/x/dex/exchange"
	keeperutil "github.com/sei-protocol/sei-chain/x/dex/keeper/utils"
	"github.com/sei-protocol/sei-chain/x/dex/types"
	"github.com/stretchr/testify/require"
	"github.com/tendermint/tendermint/libs/log"
	tmproto "github.com/tendermint/tendermint/proto/tendermint/types"
)

var TestFuzzSettleCtx = sdk.NewContext(nil, tmproto.Header{}, false, log.NewNopLogger())

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
	dexkeeper, ctx := keepertest.DexKeeper(t)
	ctx = ctx.WithBlockHeight(1).WithBlockTime(time.Now())
	entries := fuzzing.GetOrderBookEntries(!long, keepertest.TestPriceDenom, keepertest.TestAssetDenom, entryWeights, accountIndices, allocationWeights)
	for _, entry := range entries {
		if long {
			dexkeeper.SetShortOrderBookEntry(ctx, keepertest.TestContract, entry)
		} else {
			dexkeeper.SetLongOrderBookEntry(ctx, keepertest.TestContract, entry)
		}
	}
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

	orderbook := keeperutil.PopulateOrderbook(ctx, dexkeeper, types.ContractAddress(keepertest.TestContract), types.Pair{PriceDenom: keepertest.TestPriceDenom, AssetDenom: keepertest.TestAssetDenom})
	book := orderbook.Longs
	if long {
		book = orderbook.Shorts
	}
	for i, entry := range entries {
		require.NotPanics(t, func() {
			exchange.Settle(ctx, orders[i], quantity, book, price, entry.GetPrice())
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

	quantity := fuzzing.FuzzDec(quantityI, quantityIsNil)

	if len(buyEntries) > len(sellEntries) {
		buyEntries = buyEntries[:len(sellEntries)]
	} else {
		sellEntries = sellEntries[:len(buyEntries)]
	}

	orderbook := keeperutil.PopulateOrderbook(ctx, dexkeeper, types.ContractAddress(keepertest.TestContract), types.Pair{PriceDenom: keepertest.TestPriceDenom, AssetDenom: keepertest.TestAssetDenom})
	for i, longEntry := range buyEntries {
		require.NotPanics(t, func() {
			exchange.SettleFromBook(ctx, orderbook, quantity, longEntry.GetPrice(), sellEntries[i].GetPrice())
		})
	}
}
