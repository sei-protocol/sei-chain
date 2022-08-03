package exchange_test

import (
	"testing"

	"github.com/sei-protocol/sei-chain/testutil/fuzzing"
	keepertest "github.com/sei-protocol/sei-chain/testutil/keeper"
	"github.com/sei-protocol/sei-chain/x/dex/exchange"
	"github.com/stretchr/testify/require"
)

func FuzzRebalance(f *testing.F) {
	f.Fuzz(fuzzTargetMatchMarketOrders)
}

func fuzzTargetRebalance(
	t *testing.T,
	long bool,
	entryWeights []byte,
	accountIndices []byte,
	allocationWeights []byte,
) {
	entries := fuzzing.GetOrderBookEntries(long, keepertest.TestPriceDenom, keepertest.TestAssetDenom, entryWeights, accountIndices, allocationWeights)
	for _, entry := range entries {
		require.NotPanics(t, func() {
			exchange.RebalanceAllocations(entry)
		})
	}
}
