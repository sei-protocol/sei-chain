package kv

import (
	"context"
	"errors"
	"fmt"
	"math"
	"testing"

	"github.com/gogo/protobuf/proto"
	"github.com/stretchr/testify/require"
	dbm "github.com/tendermint/tm-db"

	abci "github.com/sei-protocol/sei-chain/sei-tendermint/abci/types"
	"github.com/sei-protocol/sei-chain/sei-tendermint/internal/pubsub/query"
	"github.com/sei-protocol/sei-chain/sei-tendermint/internal/state/indexer"
	"github.com/sei-protocol/sei-chain/sei-tendermint/types"
)

// hipair identifies a tx by (height, index) for order-sensitive assertions.
type hipair struct {
	h int64
	i uint32
}

func toPairs(results []*abci.TxResultV2) []hipair {
	out := make([]hipair, len(results))
	for k, r := range results {
		out[k] = hipair{r.Height, r.Index}
	}
	return out
}

// hoTx builds a tx result at (height, index) carrying app.name=sei.
func hoTx(height int64, index uint32) *abci.TxResultV2 {
	return &abci.TxResultV2{
		Height: height,
		Index:  index,
		Tx:     types.Tx(fmt.Sprintf("tx-%d-%d", height, index)),
		Result: abci.ExecTxResult{Code: abci.CodeTypeOK, Events: []abci.Event{{
			Type:       "app",
			Attributes: []abci.EventAttribute{{Key: []byte("name"), Value: []byte("sei"), Index: true}},
		}}},
	}
}

// indexLegacyOnly writes the legacy (value-ordered) index entries and the
// primary key for a tx, but not the new height-ordered keys, and does not touch
// the watermark. It simulates a pre-upgrade block that predates the
// height-ordered index (forward-fill leaves these uncovered by the new index).
func indexLegacyOnly(t *testing.T, idx *TxIndex, res *abci.TxResultV2) {
	t.Helper()
	b := idx.store.NewBatch()
	defer func() { _ = b.Close() }()

	hash := types.Tx(res.Tx).Hash()
	hashBytes := hash[:]

	for _, event := range res.Result.Events {
		if len(event.Type) == 0 {
			continue
		}
		for _, attr := range event.Attributes {
			if len(attr.Key) == 0 || !attr.GetIndex() {
				continue
			}
			compositeTag := fmt.Sprintf("%s.%s", event.Type, string(attr.Key))
			require.NoError(t, b.Set(keyFromEvent(compositeTag, string(attr.Value), res), hashBytes))
		}
	}
	require.NoError(t, b.Set(KeyFromHeight(res), hashBytes))

	rawBytes, err := proto.Marshal(&abci.TxResult{Height: res.Height, Index: res.Index, Tx: res.Tx, Result: res.Result})
	require.NoError(t, err)
	require.NoError(t, b.Set(primaryKey(hashBytes), rawBytes))
	require.NoError(t, b.WriteSync())
}

// backfillHeightOrdered writes the height-ordered keys for a previously
// legacy-only tx, simulating a background backfill (without lowering W here).
func backfillHeightOrdered(t *testing.T, idx *TxIndex, res *abci.TxResultV2) {
	t.Helper()
	b := idx.store.NewBatch()
	defer func() { _ = b.Close() }()

	hash := types.Tx(res.Tx).Hash()
	hashBytes := hash[:]
	for _, event := range res.Result.Events {
		for _, attr := range event.Attributes {
			if !attr.GetIndex() {
				continue
			}
			compositeTag := fmt.Sprintf("%s.%s", event.Type, string(attr.Key))
			require.NoError(t, b.Set(keyFromEventHeightOrdered(compositeTag, string(attr.Value), res), hashBytes))
		}
	}
	require.NoError(t, b.Set(keyFromHeightHeightOrdered(res), hashBytes))
	require.NoError(t, b.WriteSync())
}

func setWatermark(t *testing.T, idx *TxIndex, w int64) {
	t.Helper()
	require.NoError(t, idx.store.Set(watermarkKey(), int64ToBytes(w)))
}

// hoFixture builds an index with two txs per height for heights 1..10. Heights
// 1..5 are legacy-only (pre-upgrade); heights 6..10 are dual-written. The
// watermark is set to 6, so a height-ordered query splits: [6,10] from the new
// index, [1,5] from the legacy fallback.
const hoMaxHeight = 10
const hoWatermark = 6

func hoFixture(t *testing.T) *TxIndex {
	t.Helper()
	idx := NewTxIndex(dbm.NewMemDB())

	// Post-upgrade heights (dual-write) first so the watermark settles at 6,
	// then splice the pre-upgrade legacy-only heights beneath it.
	for h := int64(hoWatermark); h <= hoMaxHeight; h++ {
		for i := uint32(0); i < 2; i++ {
			require.NoError(t, idx.Index([]*abci.TxResultV2{hoTx(h, i)}))
		}
	}
	w, err := idx.readWatermark()
	require.NoError(t, err)
	require.Equal(t, int64(hoWatermark), w)

	for h := int64(1); h < hoWatermark; h++ {
		for i := uint32(0); i < 2; i++ {
			indexLegacyOnly(t, idx, hoTx(h, i))
		}
	}
	// Watermark must be unaffected by legacy-only writes.
	w, err = idx.readWatermark()
	require.NoError(t, err)
	require.Equal(t, int64(hoWatermark), w)
	return idx
}

// reference returns the expected (height,index) pairs for a query matching all
// txs with height in [lo,hi], ordered per desc and capped at limit (<=0 == all).
func reference(desc bool, lo, hi int64, limit int) []hipair {
	var out []hipair
	appendHeight := func(h int64) {
		if h < lo || h > hi {
			return
		}
		if desc {
			out = append(out, hipair{h, 1}, hipair{h, 0})
		} else {
			out = append(out, hipair{h, 0}, hipair{h, 1})
		}
	}
	if desc {
		for h := int64(hoMaxHeight); h >= 1; h-- {
			appendHeight(h)
		}
	} else {
		for h := int64(1); h <= hoMaxHeight; h++ {
			appendHeight(h)
		}
	}
	if limit > 0 && len(out) > limit {
		out = out[:limit]
	}
	return out
}

// TestTxIndexDualWrite verifies the PLT-786 contract: every indexed attribute
// is written to both the legacy value-ordered key and the new height-ordered
// key, and the watermark is advanced to the indexed height.
func TestTxIndexDualWrite(t *testing.T) {
	idx := NewTxIndex(dbm.NewMemDB())
	res := hoTx(7, 0)
	require.NoError(t, idx.Index([]*abci.TxResultV2{res}))

	// Legacy value-ordered event key present.
	legacy, err := idx.store.Has(keyFromEvent("app.name", "sei", res))
	require.NoError(t, err)
	require.True(t, legacy, "legacy event key must be written")

	// New height-ordered event key present.
	ho, err := idx.store.Has(keyFromEventHeightOrdered("app.name", "sei", res))
	require.NoError(t, err)
	require.True(t, ho, "height-ordered event key must be written")

	// tx.height dual-write present in both indexes.
	legacyHeight, err := idx.store.Has(KeyFromHeight(res))
	require.NoError(t, err)
	require.True(t, legacyHeight)
	hoHeight, err := idx.store.Has(keyFromHeightHeightOrdered(res))
	require.NoError(t, err)
	require.True(t, hoHeight)

	// Watermark advanced to the indexed height.
	w, err := idx.readWatermark()
	require.NoError(t, err)
	require.Equal(t, int64(7), w)
}

// TestTxSearchWatermarkSplit exercises the height-ordered split-merge across the
// watermark for tx.height ranges and EXISTS, at boundary lower bounds
// {W-1, W, W+1}, both orderings, and various limits. The result must always
// equal the full reference set (forward-fill leaves no gap).
func TestTxSearchWatermarkSplit(t *testing.T) {
	idx := hoFixture(t)

	cases := []struct {
		name     string
		q        string
		lo, hi   int64
		limitSet []int
	}{
		{"exists full range", `app.name EXISTS`, 1, hoMaxHeight, []int{0, 1, 3, 7, 11, 20}},
		{"height >= W-1 (5)", `tx.height >= 5`, 5, hoMaxHeight, []int{0, 1, 2, 3, 5}},
		{"height >= W (6)", `tx.height >= 6`, 6, hoMaxHeight, []int{0, 1, 4}},
		{"height >= W+1 (7)", `tx.height >= 7`, 7, hoMaxHeight, []int{0, 2, 8}},
		{"height <= W-1 (5)", `tx.height <= 5`, 1, 5, []int{0, 3}},
		{"height <= W (6)", `tx.height <= 6`, 1, 6, []int{0, 3}},
		{"height window straddling W", `tx.height >= 4 AND tx.height <= 7`, 4, 7, []int{0, 2, 5}},
	}

	for _, tc := range cases {
		for _, desc := range []bool{true, false} {
			for _, limit := range tc.limitSet {
				name := fmt.Sprintf("%s/desc=%v/limit=%d", tc.name, desc, limit)
				t.Run(name, func(t *testing.T) {
					results, err := idx.Search(t.Context(), query.MustCompile(tc.q),
						indexer.SearchOptions{Limit: limit, OrderDesc: desc, MaxScan: 0})
					require.NoError(t, err)
					require.Equal(t, reference(desc, tc.lo, tc.hi, limit), toPairs(results))
				})
			}
		}
	}
}

// TestTxSearchWatermarkUnset verifies that an unset watermark (fresh /
// upgraded-but-not-written DB) routes every height-ordered query to the legacy
// fallback and still returns the full set.
func TestTxSearchWatermarkUnset(t *testing.T) {
	idx := NewTxIndex(dbm.NewMemDB())
	// Write only legacy entries and force the watermark to +inf.
	for h := int64(1); h <= 5; h++ {
		indexLegacyOnly(t, idx, hoTx(h, 0))
	}
	w, err := idx.readWatermark()
	require.NoError(t, err)
	require.Equal(t, int64(math.MaxInt64), w)

	results, err := idx.Search(t.Context(), query.MustCompile(`app.name EXISTS`),
		indexer.SearchOptions{OrderDesc: true})
	require.NoError(t, err)

	want := []hipair{{5, 0}, {4, 0}, {3, 0}, {2, 0}, {1, 0}}
	require.Equal(t, want, toPairs(results))
}

// TestTxSearchBackfillLowersWatermark verifies that backfilling the sub-watermark
// heights into the new index and lowering W keeps results correct and does not
// double-count across the split.
func TestTxSearchBackfillLowersWatermark(t *testing.T) {
	idx := hoFixture(t)

	q := query.MustCompile(`app.name EXISTS`)
	before, err := idx.Search(t.Context(), q, indexer.SearchOptions{OrderDesc: true})
	require.NoError(t, err)
	require.Equal(t, reference(true, 1, hoMaxHeight, 0), toPairs(before))

	// Backfill heights 1..5 into the new index and lower the watermark to 1.
	for h := int64(1); h < hoWatermark; h++ {
		for i := uint32(0); i < 2; i++ {
			backfillHeightOrdered(t, idx, hoTx(h, i))
		}
	}
	setWatermark(t, idx, 1)

	after, err := idx.Search(t.Context(), q, indexer.SearchOptions{OrderDesc: true})
	require.NoError(t, err)
	// Same set, no duplicates introduced by the now-all-fast-path scan.
	require.Equal(t, reference(true, 1, hoMaxHeight, 0), toPairs(after))
}

// TestTxSearchScanBudgetFailClosed asserts that a fallback query exceeding the
// scan budget fails closed with ErrSearchScanBudgetExceeded (no partial), for
// both orderings and single- and multi-condition queries, while a query within
// budget succeeds.
func TestTxSearchScanBudgetFailClosed(t *testing.T) {
	idx := NewTxIndex(dbm.NewMemDB())
	// 12 txs, each carrying app.name=sei so the CONTAINS fallback must scan all.
	for h := int64(1); h <= 12; h++ {
		res := hoTx(h, 0)
		res.Result.Events = append(res.Result.Events, abci.Event{
			Type:       "app",
			Attributes: []abci.EventAttribute{{Key: []byte("kind"), Value: []byte("even"), Index: true}},
		})
		require.NoError(t, idx.Index([]*abci.TxResultV2{res}))
	}

	for _, desc := range []bool{true, false} {
		for _, q := range []string{
			`app.name CONTAINS 'se'`,
			`app.name CONTAINS 'se' AND app.kind CONTAINS 'ev'`,
		} {
			t.Run(fmt.Sprintf("desc=%v/%s", desc, q), func(t *testing.T) {
				results, err := idx.Search(t.Context(), query.MustCompile(q),
					indexer.SearchOptions{Limit: 5, OrderDesc: desc, MaxScan: 3})
				require.ErrorIs(t, err, indexer.ErrSearchScanBudgetExceeded)
				require.Nil(t, results)
			})
		}
	}

	// Within budget (or disabled) the same query succeeds.
	results, err := idx.Search(t.Context(), query.MustCompile(`app.name CONTAINS 'se'`),
		indexer.SearchOptions{Limit: 5, OrderDesc: true, MaxScan: 0})
	require.NoError(t, err)
	require.Len(t, results, 5)
}

// TestTxSearchBudgetVsDeadline asserts the two truncation signals stay distinct:
// a cancelled context yields a partial result with a nil error, whereas an
// exceeded budget yields an explicit error.
func TestTxSearchBudgetVsDeadline(t *testing.T) {
	idx := NewTxIndex(dbm.NewMemDB())
	for h := int64(1); h <= 12; h++ {
		require.NoError(t, idx.Index([]*abci.TxResultV2{hoTx(h, 0)}))
	}

	// Deadline: cancelled context -> nil error.
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	results, err := idx.Search(ctx, query.MustCompile(`app.name CONTAINS 'se'`),
		indexer.SearchOptions{Limit: 5, OrderDesc: true, MaxScan: 3})
	require.NoError(t, err)
	require.Empty(t, results)

	// Budget: fresh context, tight budget -> explicit error.
	_, err = idx.Search(t.Context(), query.MustCompile(`app.name CONTAINS 'se'`),
		indexer.SearchOptions{Limit: 5, OrderDesc: true, MaxScan: 3})
	require.True(t, errors.Is(err, indexer.ErrSearchScanBudgetExceeded))
}

// TestTxReindexNoWatermark verifies dual-writing the height-ordered index and not changing watermark
func TestTxReindexNoWatermark(t *testing.T) {
	store := dbm.NewMemDB()

	ri := NewTxIndexSkipWatermark(store)
	for h := int64(1); h <= 5; h++ {
		require.NoError(t, ri.Index([]*abci.TxResultV2{hoTx(h, 0)}))
	}
	w, err := ri.readWatermark()
	require.NoError(t, err)
	require.Equal(t, int64(math.MaxInt64), w, "reindex must not establish the watermark")

	live := NewTxIndex(store)
	require.NoError(t, live.Index([]*abci.TxResultV2{hoTx(6, 0)}))
	w, err = live.readWatermark()
	require.NoError(t, err)
	require.Equal(t, int64(6), w, "live indexing anchors the watermark forward")
}

// TestTxWatermarkAccessor verifies the exported Watermark accessor reports the anchored height correctly.
func TestTxWatermarkAccessor(t *testing.T) {
	idx := NewTxIndex(dbm.NewMemDB())

	h, set, err := idx.Watermark()
	require.NoError(t, err)
	require.False(t, set, "watermark must read as unset on a fresh DB")
	require.Zero(t, h)

	require.NoError(t, idx.Index([]*abci.TxResultV2{hoTx(7, 0)}))

	h, set, err = idx.Watermark()
	require.NoError(t, err)
	require.True(t, set)
	require.Equal(t, int64(7), h)
}

// TestTxReindexDoesNotLowerAnchoredWatermark ensures partial re-indexing will not lower the height-ordered index watermark
func TestTxReindexDoesNotLowerAnchoredWatermark(t *testing.T) {
	store := dbm.NewMemDB()

	live := NewTxIndex(store)
	for h := int64(hoWatermark); h <= hoMaxHeight; h++ {
		require.NoError(t, live.Index([]*abci.TxResultV2{hoTx(h, 0)}))
	}
	w, err := live.readWatermark()
	require.NoError(t, err)
	require.Equal(t, int64(hoWatermark), w)

	ri := NewTxIndexSkipWatermark(store)
	for h := int64(1); h < hoWatermark; h++ {
		require.NoError(t, ri.Index([]*abci.TxResultV2{hoTx(h, 0)}))
	}
	w, err = ri.readWatermark()
	require.NoError(t, err)
	require.Equal(t, int64(hoWatermark), w, "partial reindex below the watermark must not lower it")
}
