package kv_test

import (
	"context"
	"fmt"
	"testing"

	"github.com/stretchr/testify/require"
	dbm "github.com/tendermint/tm-db"

	abci "github.com/sei-protocol/sei-chain/sei-tendermint/abci/types"
	"github.com/sei-protocol/sei-chain/sei-tendermint/internal/pubsub/query"
	"github.com/sei-protocol/sei-chain/sei-tendermint/internal/state/indexer"
	blockidxkv "github.com/sei-protocol/sei-chain/sei-tendermint/internal/state/indexer/block/kv"
	"github.com/sei-protocol/sei-chain/sei-tendermint/types"
)

// searchOpts is the zero-value (unbounded, ascending) options used by the
// existing block-indexer tests.
var searchOpts = indexer.SearchOptions{}

func TestBlockIndexer(t *testing.T) {
	store := dbm.NewPrefixDB(dbm.NewMemDB(), []byte("block_events"))
	indexer := blockidxkv.New(store)

	require.NoError(t, indexer.Index(types.EventDataNewBlockHeader{
		Header: types.Header{Height: 1},
		ResultFinalizeBlock: abci.ResponseFinalizeBlock{
			Events: []abci.Event{
				{
					Type: "finalize_event1",
					Attributes: []abci.EventAttribute{
						{
							Key:   []byte("proposer"),
							Value: []byte("FCAA001"),
							Index: true,
						},
					},
				},
				{
					Type: "finalize_event2",
					Attributes: []abci.EventAttribute{
						{
							Key:   []byte("foo"),
							Value: []byte("100"),
							Index: true,
						},
					},
				},
			},
		},
	}))

	for i := 2; i < 12; i++ {
		var index bool
		if i%2 == 0 {
			index = true
		}
		require.NoError(t, indexer.Index(types.EventDataNewBlockHeader{
			Header: types.Header{Height: int64(i)},
			ResultFinalizeBlock: abci.ResponseFinalizeBlock{
				Events: []abci.Event{
					{
						Type: "finalize_event1",
						Attributes: []abci.EventAttribute{
							{
								Key:   []byte("proposer"),
								Value: []byte("FCAA001"),
								Index: true,
							},
						},
					},
					{
						Type: "finalize_event2",
						Attributes: []abci.EventAttribute{
							{
								Key:   []byte("foo"),
								Value: []byte(fmt.Sprintf("%d", i)),
								Index: index,
							},
						},
					},
				},
			},
		}))
	}

	testCases := map[string]struct {
		q       *query.Query
		results []int64
	}{
		"block.height = 100": {
			q:       query.MustCompile(`block.height = 100`),
			results: []int64{},
		},
		"block.height = 5": {
			q:       query.MustCompile(`block.height = 5`),
			results: []int64{5},
		},
		"finalize_event.key1 = 'value1'": {
			q:       query.MustCompile(`finalize_event1.key1 = 'value1'`),
			results: []int64{},
		},
		"finalize_event.proposer = 'FCAA001'": {
			q:       query.MustCompile(`finalize_event1.proposer = 'FCAA001'`),
			results: []int64{1, 2, 3, 4, 5, 6, 7, 8, 9, 10, 11},
		},
		"finalize_event.foo <= 5": {
			q:       query.MustCompile(`finalize_event2.foo <= 5`),
			results: []int64{2, 4},
		},
		"finalize_event.foo >= 100": {
			q:       query.MustCompile(`finalize_event2.foo >= 100`),
			results: []int64{1},
		},
		"block.height > 2 AND finalize_event2.foo <= 8": {
			q:       query.MustCompile(`block.height > 2 AND finalize_event2.foo <= 8`),
			results: []int64{4, 6, 8},
		},
		"finalize_event.proposer CONTAINS 'FFFFFFF'": {
			q:       query.MustCompile(`finalize_event1.proposer CONTAINS 'FFFFFFF'`),
			results: []int64{},
		},
		"finalize_event.proposer CONTAINS 'FCAA001'": {
			q:       query.MustCompile(`finalize_event1.proposer CONTAINS 'FCAA001'`),
			results: []int64{1, 2, 3, 4, 5, 6, 7, 8, 9, 10, 11},
		},
		"finalize_event.proposer MATCHES '.*FF.*'": {
			q:       query.MustCompile(`finalize_event1.proposer MATCHES '.*FF.*'`),
			results: []int64{},
		},
		"finalize_event.proposer MATCHES '.*F.*'": {
			q:       query.MustCompile(`finalize_event1.proposer MATCHES '.*F.*'`),
			results: []int64{1, 2, 3, 4, 5, 6, 7, 8, 9, 10, 11},
		},
	}

	for name, tc := range testCases {
		t.Run(name, func(t *testing.T) {
			ctx := t.Context()

			results, err := indexer.Search(ctx, tc.q, searchOpts)
			require.NoError(t, err)
			require.Equal(t, tc.results, results)
		})
	}
}

// TestBlockIndexerBounded exercises the bounded/ordered Search path:
// the limit and order_by are pushed into the scan so a broad query returns the
// correct top-N without materializing and sorting the full match set.
func TestBlockIndexerBounded(t *testing.T) {
	store := dbm.NewPrefixDB(dbm.NewMemDB(), []byte("block_events_bounded"))
	idx := blockidxkv.New(store)

	// Heights 1..10 all carry app.name=sei; even heights also carry
	// app.kind=even.
	for i := int64(1); i <= 10; i++ {
		events := []abci.Event{{
			Type: "app",
			Attributes: []abci.EventAttribute{
				{Key: []byte("name"), Value: []byte("sei"), Index: true},
			},
		}}
		if i%2 == 0 {
			events = append(events, abci.Event{
				Type: "app",
				Attributes: []abci.EventAttribute{
					{Key: []byte("kind"), Value: []byte("even"), Index: true},
				},
			})
		}
		require.NoError(t, idx.Index(types.EventDataNewBlockHeader{
			Header:              types.Header{Height: i},
			ResultFinalizeBlock: abci.ResponseFinalizeBlock{Events: events},
		}))
	}

	testCases := map[string]struct {
		q       string
		opts    indexer.SearchOptions
		results []int64
	}{
		// Fast path: single equality driver, scanned in order_by order and
		// capped at the scan rather than after a full sort.
		"equality desc limit 3": {
			q:       `app.name = 'sei'`,
			opts:    indexer.SearchOptions{Limit: 3, OrderDesc: true},
			results: []int64{10, 9, 8},
		},
		"equality asc limit 3": {
			q:       `app.name = 'sei'`,
			opts:    indexer.SearchOptions{Limit: 3, OrderDesc: false},
			results: []int64{1, 2, 3},
		},
		// A disabled cap (Limit <= 0) returns the full set in order_by order.
		"equality desc unbounded": {
			q:       `app.name = 'sei'`,
			opts:    indexer.SearchOptions{Limit: 0, OrderDesc: true},
			results: []int64{10, 9, 8, 7, 6, 5, 4, 3, 2, 1},
		},
		// Fast path: two equalities — driver plus a point-probe.
		"two equalities desc limit 2": {
			q:       `app.name = 'sei' AND app.kind = 'even'`,
			opts:    indexer.SearchOptions{Limit: 2, OrderDesc: true},
			results: []int64{10, 8},
		},
		// Fast path: equality driver with a block.height range filter.
		"equality and height range desc limit 2": {
			q:       `app.name = 'sei' AND block.height >= 6`,
			opts:    indexer.SearchOptions{Limit: 2, OrderDesc: true},
			results: []int64{10, 9},
		},
		// Fast path: pure block.height range driver off the primary key.
		"height range desc limit 3": {
			q:       `block.height >= 6`,
			opts:    indexer.SearchOptions{Limit: 3, OrderDesc: true},
			results: []int64{10, 9, 8},
		},
		// Fast path: pure block.height range driver scanned ascending.
		"height range asc limit 3": {
			q:       `block.height >= 6`,
			opts:    indexer.SearchOptions{Limit: 3, OrderDesc: false},
			results: []int64{6, 7, 8},
		},
		// Fast path: a dual-bounded block.height range (lower AND upper bound).
		"height range dual-bounded desc unbounded": {
			q:       `block.height >= 4 AND block.height <= 7`,
			opts:    indexer.SearchOptions{Limit: 0, OrderDesc: true},
			results: []int64{7, 6, 5, 4},
		},
		// Fallback path: CONTAINS cannot be point-probed, but the result set is
		// still ordered and capped.
		"contains fallback desc limit 3": {
			q:       `app.name CONTAINS 'se'`,
			opts:    indexer.SearchOptions{Limit: 3, OrderDesc: true},
			results: []int64{10, 9, 8},
		},
		"contains fallback asc limit 2": {
			q:       `app.name CONTAINS 'se'`,
			opts:    indexer.SearchOptions{Limit: 2, OrderDesc: false},
			results: []int64{1, 2},
		},
	}

	for name, tc := range testCases {
		t.Run(name, func(t *testing.T) {
			results, err := idx.Search(t.Context(), query.MustCompile(tc.q), tc.opts)
			require.NoError(t, err)
			require.Equal(t, tc.results, results)
		})
	}

	// A cancelled context makes the bounded scan return the results gathered so
	// far without an error, rather than failing the whole query.
	t.Run("cancelled context returns partial results", func(t *testing.T) {
		ctx, cancel := context.WithCancel(t.Context())
		cancel()
		results, err := idx.Search(ctx, query.MustCompile(`app.name = 'sei'`), indexer.SearchOptions{Limit: 5, OrderDesc: true})
		require.NoError(t, err)
		require.Empty(t, results)
	})
}
