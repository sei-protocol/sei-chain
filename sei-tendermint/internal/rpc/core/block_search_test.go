package core

import (
	"testing"

	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"

	"github.com/sei-protocol/sei-chain/sei-tendermint/config"
	"github.com/sei-protocol/sei-chain/sei-tendermint/internal/state/indexer"
	indexermocks "github.com/sei-protocol/sei-chain/sei-tendermint/internal/state/indexer/mocks"
	statemocks "github.com/sei-protocol/sei-chain/sei-tendermint/internal/state/mocks"
	"github.com/sei-protocol/sei-chain/sei-tendermint/rpc/coretypes"
)

func makeBlockHeights(count int) []int64 {
	heights := make([]int64, count)
	for i := 0; i < count; i++ {
		heights[i] = int64(i + 1)
	}
	return heights
}

func blockSearchEnv(t *testing.T, maxResults int, heights []int64) *Environment {
	t.Helper()
	sink := indexermocks.NewEventSink(t)
	sink.On("Type").Return(indexer.KV)
	sink.On("SearchBlockEvents", mock.Anything, mock.Anything).Return(heights, nil)

	// LoadBlock returns nil so the page loop skips materialisation; TotalCount
	// is set before the loop and reflects the cap correctly regardless.
	bs := statemocks.NewBlockStore(t)
	bs.On("LoadBlock", mock.AnythingOfType("int64")).Return(nil).Maybe()

	return &Environment{
		EventSinks: []indexer.EventSink{sink},
		BlockStore: bs,
		Config:     config.RPCConfig{MaxTxSearchResults: maxResults},
	}
}

// TestBlockSearchCapAppliedAfterSort mirrors the TxSearch correctness test:
// cap must preserve the sort order, not truncate before ordering.
// With order_by=desc on heights [1..20] the expected top-5 are 20,19,18,17,16.
// TotalCount reflects the post-cap, post-sort count.
func TestBlockSearchCapAppliedAfterSort(t *testing.T) {
	const (
		total    = 20
		capacity = 5
	)

	sink := indexermocks.NewEventSink(t)
	sink.On("Type").Return(indexer.KV)
	sink.On("SearchBlockEvents", mock.Anything, mock.Anything).Return(makeBlockHeights(total), nil)

	var loadedHeights []int64
	bs := statemocks.NewBlockStore(t)
	bs.On("LoadBlock", mock.AnythingOfType("int64")).Run(func(args mock.Arguments) {
		loadedHeights = append(loadedHeights, args.Get(0).(int64))
	}).Return(nil)

	env := &Environment{
		EventSinks: []indexer.EventSink{sink},
		BlockStore: bs,
		Config:     config.RPCConfig{MaxTxSearchResults: capacity},
	}

	res, err := env.BlockSearch(t.Context(), &coretypes.RequestBlockSearch{
		Query:   "block.height > 0",
		OrderBy: "desc",
	})
	require.NoError(t, err)
	require.Equal(t, capacity, res.TotalCount)
	require.Equal(t, []int64{20, 19, 18, 17, 16}, loadedHeights)
}

func TestBlockSearchCapDisabled(t *testing.T) {
	const total = 20

	env := blockSearchEnv(t, 0, makeBlockHeights(total))
	res, err := env.BlockSearch(t.Context(), &coretypes.RequestBlockSearch{
		Query: "block.height > 0",
	})
	require.NoError(t, err)
	require.Equal(t, total, res.TotalCount)
}

func TestBlockSearchCapUnderLimit(t *testing.T) {
	const total = 3
	const cap = 10

	env := blockSearchEnv(t, cap, makeBlockHeights(total))
	res, err := env.BlockSearch(t.Context(), &coretypes.RequestBlockSearch{
		Query: "block.height > 0",
	})
	require.NoError(t, err)
	require.Equal(t, total, res.TotalCount)
}
