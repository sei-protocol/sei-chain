package core

import (
	"testing"

	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"

	abci "github.com/sei-protocol/sei-chain/sei-tendermint/abci/types"
	"github.com/sei-protocol/sei-chain/sei-tendermint/config"
	"github.com/sei-protocol/sei-chain/sei-tendermint/internal/state/indexer"
	indexermocks "github.com/sei-protocol/sei-chain/sei-tendermint/internal/state/indexer/mocks"
	"github.com/sei-protocol/sei-chain/sei-tendermint/rpc/coretypes"
)

func makeTxResults(count int) []*abci.TxResultV2 {
	results := make([]*abci.TxResultV2, count)
	for i := 0; i < count; i++ {
		results[i] = &abci.TxResultV2{Height: int64(i + 1), Index: 0}
	}
	return results
}

func txSearchEnv(t *testing.T, maxResults int, txs []*abci.TxResultV2) *Environment {
	t.Helper()
	sink := indexermocks.NewEventSink(t)
	sink.On("Type").Return(indexer.KV)
	sink.On("SearchTxEvents", mock.Anything, mock.Anything, mock.Anything).Return(txs, nil)
	return &Environment{
		EventSinks: []indexer.EventSink{sink},
		Config:     config.RPCConfig{MaxTxSearchResults: maxResults},
	}
}

// TestTxSearchCapAppliedAfterSort is the core correctness test: cap must keep
// the top-N by order_by, not an arbitrary prefix of the indexer output.
func TestTxSearchCapAppliedAfterSort(t *testing.T) {
	const total = 20
	const cap = 5

	env := txSearchEnv(t, cap, makeTxResults(total))
	res, err := env.TxSearch(t.Context(), &coretypes.RequestTxSearch{
		Query:   "tx.height > 0",
		OrderBy: "desc",
	})
	require.NoError(t, err)
	require.Len(t, res.Txs, cap)
	require.Equal(t, cap, res.TotalCount)
	// desc: expect heights total, total-1, ..., total-cap+1
	for i, tx := range res.Txs {
		require.Equal(t, int64(total-i), tx.Height)
	}
}

func TestTxSearchCapDisabled(t *testing.T) {
	const total = 20

	env := txSearchEnv(t, 0, makeTxResults(total))
	res, err := env.TxSearch(t.Context(), &coretypes.RequestTxSearch{
		Query:   "tx.height > 0",
		OrderBy: "asc",
	})
	require.NoError(t, err)
	require.Len(t, res.Txs, total)
	require.Equal(t, total, res.TotalCount)
}

func TestTxSearchCapUnderLimit(t *testing.T) {
	const total = 3
	const cap = 10

	env := txSearchEnv(t, cap, makeTxResults(total))
	res, err := env.TxSearch(t.Context(), &coretypes.RequestTxSearch{
		Query: "tx.height > 0",
	})
	require.NoError(t, err)
	require.Len(t, res.Txs, total)
	require.Equal(t, total, res.TotalCount)
}
