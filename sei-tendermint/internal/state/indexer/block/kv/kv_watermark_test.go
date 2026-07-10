package kv

import (
	"context"
	"fmt"
	"math"
	"testing"

	"github.com/stretchr/testify/require"
	dbm "github.com/tendermint/tm-db"

	abci "github.com/sei-protocol/sei-chain/sei-tendermint/abci/types"
	"github.com/sei-protocol/sei-chain/sei-tendermint/internal/pubsub/query"
	"github.com/sei-protocol/sei-chain/sei-tendermint/internal/state/indexer"
	"github.com/sei-protocol/sei-chain/sei-tendermint/types"
)

const (
	hoMaxHeight = 10
	hoWatermark = 6
	hoTyp       = "finalize_block"
)

// blockHeaderWithHeight builds a block header at the given height carrying app.name=sei.
func blockHeaderWithHeight(height int64) types.EventDataNewBlockHeader {
	return types.EventDataNewBlockHeader{
		Header: types.Header{Height: height},
		ResultFinalizeBlock: abci.ResponseFinalizeBlock{Events: []abci.Event{{
			Type:       "app",
			Attributes: []abci.EventAttribute{{Key: []byte("name"), Value: []byte("sei"), Index: true}},
		}}},
	}
}

// indexLegacyOnly writes the primary height key and the legacy (value-ordered)
// event keys for a block, but not the new height-ordered keys, and does not
// touch the watermark — simulating a pre-upgrade block uncovered by the new
// index.
func indexLegacyOnly(t *testing.T, idx *BlockerIndexer, bh types.EventDataNewBlockHeader) {
	t.Helper()
	b := idx.store.NewBatch()
	defer func() { _ = b.Close() }()

	height := bh.Header.Height
	hk, err := heightKey(height)
	require.NoError(t, err)
	require.NoError(t, b.Set(hk, int64ToBytes(height)))

	for _, event := range bh.ResultFinalizeBlock.Events {
		for _, attr := range event.Attributes {
			if !attr.GetIndex() {
				continue
			}
			compositeKey := fmt.Sprintf("%s.%s", event.Type, string(attr.Key))
			k, err := eventKey(compositeKey, hoTyp, string(attr.Value), height)
			require.NoError(t, err)
			require.NoError(t, b.Set(k, int64ToBytes(height)))
		}
	}
	require.NoError(t, b.WriteSync())
}

// backfillHeightOrdered writes the height-ordered keys for a previously
// legacy-only block, simulating a background backfill (without lowering W here).
func backfillHeightOrdered(t *testing.T, idx *BlockerIndexer, bh types.EventDataNewBlockHeader) {
	t.Helper()
	b := idx.store.NewBatch()
	defer func() { _ = b.Close() }()

	height := bh.Header.Height
	for _, event := range bh.ResultFinalizeBlock.Events {
		for _, attr := range event.Attributes {
			if !attr.GetIndex() {
				continue
			}
			compositeKey := fmt.Sprintf("%s.%s", event.Type, string(attr.Key))
			k, err := eventKeyHeightOrdered(compositeKey, hoTyp, string(attr.Value), height)
			require.NoError(t, err)
			require.NoError(t, b.Set(k, int64ToBytes(height)))
		}
	}
	require.NoError(t, b.WriteSync())
}

func setWatermark(t *testing.T, idx *BlockerIndexer, w int64) {
	t.Helper()
	key, err := watermarkKey()
	require.NoError(t, err)
	require.NoError(t, idx.store.Set(key, int64ToBytes(w)))
}

// hoFixture indexes heights 6..10 with the dual-write index (watermark settles
// at 6) and splices legacy-only heights 1..5 beneath it, so a height-ordered
// EXISTS query must split: [6,10] from the new index, [1,5] from the legacy
// fallback.
func hoFixture(t *testing.T) *BlockerIndexer {
	t.Helper()
	idx := New(dbm.NewMemDB())

	for h := int64(hoWatermark); h <= hoMaxHeight; h++ {
		require.NoError(t, idx.Index(blockHeaderWithHeight(h)))
	}
	w, err := idx.readWatermark()
	require.NoError(t, err)
	require.Equal(t, int64(hoWatermark), w)

	for h := int64(1); h < hoWatermark; h++ {
		indexLegacyOnly(t, idx, blockHeaderWithHeight(h))
	}
	w, err = idx.readWatermark()
	require.NoError(t, err)
	require.Equal(t, int64(hoWatermark), w)
	return idx
}

// reference returns the heights in [lo,hi] (intersected with [1,hoMaxHeight]),
// ordered per desc and capped at limit (<=0 == all).
func reference(desc bool, lo, hi int64, limit int) []int64 {
	out := []int64{}
	if desc {
		for h := int64(hoMaxHeight); h >= 1; h-- {
			if h >= lo && h <= hi {
				out = append(out, h)
			}
		}
	} else {
		for h := int64(1); h <= hoMaxHeight; h++ {
			if h >= lo && h <= hi {
				out = append(out, h)
			}
		}
	}
	if limit > 0 && len(out) > limit {
		out = out[:limit]
	}
	return out
}

// TestBlockSearchWatermarkSplit exercises the EXISTS height-ordered split-merge
// across the watermark, at boundary lower bounds {W-1, W, W+1}, both orderings
// and various limits. Block.height ranges are served by the primary key and are
// not routed here, so the split is driven by EXISTS (optionally + a height
// range).
func TestBlockSearchWatermarkSplit(t *testing.T) {
	idx := hoFixture(t)

	cases := []struct {
		name     string
		q        string
		lo, hi   int64
		limitSet []int
	}{
		{"exists full range", `app.name EXISTS`, 1, hoMaxHeight, []int{0, 1, 3, 5, 11}},
		{"exists height >= W-1 (5)", `app.name EXISTS AND block.height >= 5`, 5, hoMaxHeight, []int{0, 1, 2, 5}},
		{"exists height >= W (6)", `app.name EXISTS AND block.height >= 6`, 6, hoMaxHeight, []int{0, 1, 4}},
		{"exists height >= W+1 (7)", `app.name EXISTS AND block.height >= 7`, 7, hoMaxHeight, []int{0, 2}},
		{"exists height <= W-1 (5)", `app.name EXISTS AND block.height <= 5`, 1, 5, []int{0, 3}},
		{"exists window straddling W", `app.name EXISTS AND block.height >= 4 AND block.height <= 7`, 4, 7, []int{0, 2}},
	}

	for _, tc := range cases {
		for _, desc := range []bool{true, false} {
			for _, limit := range tc.limitSet {
				name := fmt.Sprintf("%s/desc=%v/limit=%d", tc.name, desc, limit)
				t.Run(name, func(t *testing.T) {
					results, err := idx.Search(t.Context(), query.MustCompile(tc.q),
						indexer.SearchOptions{Limit: limit, OrderDesc: desc, MaxScan: 0})
					require.NoError(t, err)
					require.Equal(t, reference(desc, tc.lo, tc.hi, limit), results)
				})
			}
		}
	}
}

// TestBlockSearchWatermarkUnset verifies that an unset watermark routes an
// EXISTS query entirely to the legacy fallback and still returns the full set.
func TestBlockSearchWatermarkUnset(t *testing.T) {
	idx := New(dbm.NewMemDB())
	for h := int64(1); h <= 5; h++ {
		indexLegacyOnly(t, idx, blockHeaderWithHeight(h))
	}
	w, err := idx.readWatermark()
	require.NoError(t, err)
	require.Equal(t, int64(math.MaxInt64), w)

	results, err := idx.Search(t.Context(), query.MustCompile(`app.name EXISTS`),
		indexer.SearchOptions{OrderDesc: true})
	require.NoError(t, err)
	require.Equal(t, []int64{5, 4, 3, 2, 1}, results)
}

// TestBlockSearchBackfillLowersWatermark verifies that backfilling the
// sub-watermark heights and lowering W keeps results correct with no
// double-count across the split.
func TestBlockSearchBackfillLowersWatermark(t *testing.T) {
	idx := hoFixture(t)

	q := query.MustCompile(`app.name EXISTS`)
	before, err := idx.Search(t.Context(), q, indexer.SearchOptions{OrderDesc: true})
	require.NoError(t, err)
	require.Equal(t, reference(true, 1, hoMaxHeight, 0), before)

	for h := int64(1); h < hoWatermark; h++ {
		backfillHeightOrdered(t, idx, blockHeaderWithHeight(h))
	}
	setWatermark(t, idx, 1)

	after, err := idx.Search(t.Context(), q, indexer.SearchOptions{OrderDesc: true})
	require.NoError(t, err)
	require.Equal(t, reference(true, 1, hoMaxHeight, 0), after)
}

// TestBlockSearchScanBudgetFailClosed asserts that a fallback query exceeding
// the scan budget fails closed with ErrSearchScanBudgetExceeded (no partial),
// while a query within budget succeeds.
func TestBlockSearchScanBudgetFailClosed(t *testing.T) {
	idx := New(dbm.NewMemDB())
	for h := int64(1); h <= 12; h++ {
		require.NoError(t, idx.Index(blockHeaderWithHeight(h)))
	}

	for _, desc := range []bool{true, false} {
		t.Run(fmt.Sprintf("desc=%v", desc), func(t *testing.T) {
			results, err := idx.Search(t.Context(), query.MustCompile(`app.name CONTAINS 'se'`),
				indexer.SearchOptions{Limit: 5, OrderDesc: desc, MaxScan: 3})
			require.ErrorIs(t, err, indexer.ErrSearchScanBudgetExceeded)
			require.Nil(t, results)
		})
	}

	results, err := idx.Search(t.Context(), query.MustCompile(`app.name CONTAINS 'se'`),
		indexer.SearchOptions{Limit: 5, OrderDesc: true, MaxScan: 0})
	require.NoError(t, err)
	require.Len(t, results, 5)
}

// TestBlockSearchBudgetVsDeadline asserts the two truncation signals stay
// distinct: a cancelled context yields a partial result with a nil error,
// whereas an exceeded budget yields an explicit error.
func TestBlockSearchBudgetVsDeadline(t *testing.T) {
	idx := New(dbm.NewMemDB())
	for h := int64(1); h <= 12; h++ {
		require.NoError(t, idx.Index(blockHeaderWithHeight(h)))
	}

	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	results, err := idx.Search(ctx, query.MustCompile(`app.name CONTAINS 'se'`),
		indexer.SearchOptions{Limit: 5, OrderDesc: true, MaxScan: 3})
	require.NoError(t, err)
	require.Empty(t, results)

	_, err = idx.Search(t.Context(), query.MustCompile(`app.name CONTAINS 'se'`),
		indexer.SearchOptions{Limit: 5, OrderDesc: true, MaxScan: 3})
	require.ErrorIs(t, err, indexer.ErrSearchScanBudgetExceeded)
}

// TestBlockReindexNoWatermark verifies dual-writing the height-ordered index and not changing watermark
func TestBlockReindexNoWatermark(t *testing.T) {
	store := dbm.NewMemDB()

	ri := NewSkipWatermark(store)
	for h := int64(1); h <= 5; h++ {
		require.NoError(t, ri.Index(blockHeaderWithHeight(h)))
	}
	w, err := ri.readWatermark()
	require.NoError(t, err)
	require.Equal(t, int64(math.MaxInt64), w, "reindex must not establish the watermark")

	live := New(store)
	require.NoError(t, live.Index(blockHeaderWithHeight(6)))
	w, err = live.readWatermark()
	require.NoError(t, err)
	require.Equal(t, int64(6), w, "live indexing anchors the watermark forward")
}

// TestBlockWatermarkAccessor verifies the exported Watermark accessor reports the anchored height correctly
func TestBlockWatermarkAccessor(t *testing.T) {
	idx := New(dbm.NewMemDB())

	h, set, err := idx.Watermark()
	require.NoError(t, err)
	require.False(t, set, "watermark must read as unset on a fresh DB")
	require.Zero(t, h)

	require.NoError(t, idx.Index(blockHeaderWithHeight(7)))

	h, set, err = idx.Watermark()
	require.NoError(t, err)
	require.True(t, set)
	require.Equal(t, int64(7), h)
}

// TestBlockReindexDoesNotLowerAnchoredWatermark ensures partial re-indexing will not lower the height-ordered index watermark
func TestBlockReindexDoesNotLowerAnchoredWatermark(t *testing.T) {
	store := dbm.NewMemDB()

	live := New(store)
	for h := int64(hoWatermark); h <= hoMaxHeight; h++ {
		require.NoError(t, live.Index(blockHeaderWithHeight(h)))
	}
	w, err := live.readWatermark()
	require.NoError(t, err)
	require.Equal(t, int64(hoWatermark), w)

	ri := NewSkipWatermark(store)
	for h := int64(1); h < hoWatermark; h++ {
		require.NoError(t, ri.Index(blockHeaderWithHeight(h)))
	}
	w, err = ri.readWatermark()
	require.NoError(t, err)
	require.Equal(t, int64(hoWatermark), w, "partial reindex below the watermark must not lower it")
}
