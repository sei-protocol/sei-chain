package kv

import (
	"context"
	"errors"
	"fmt"
	"math"
	"regexp"
	"sort"
	"strconv"
	"strings"

	"github.com/google/orderedcode"
	dbm "github.com/tendermint/tm-db"

	abci "github.com/sei-protocol/sei-chain/sei-tendermint/abci/types"
	"github.com/sei-protocol/sei-chain/sei-tendermint/internal/pubsub/query"
	"github.com/sei-protocol/sei-chain/sei-tendermint/internal/pubsub/query/syntax"
	"github.com/sei-protocol/sei-chain/sei-tendermint/internal/state/indexer"
	"github.com/sei-protocol/sei-chain/sei-tendermint/types"
)

var _ indexer.BlockIndexer = (*BlockerIndexer)(nil)

const (
	// blockHeightOrderedKey namespaces the height-ordered event index. Its keys
	// have the form orderedcode(blockHeightOrderedKey, tag, height, value, typ),
	// so within a tag they sort by height — matching the order block_search
	// returns — and stay disjoint from the legacy value-ordered index that
	// shares this store. It is reserved: events may not use it as a composite
	// key. block.height range queries are already served in height order by the
	// primary key and do not use this index.
	blockHeightOrderedKey = "block.height_ordered"

	// blockWatermarkKey stores the lowest block height covered by the
	// height-ordered index on this node (see readWatermark). It is reserved.
	blockWatermarkKey = "block.new_index_min_height"
)

// BlockerIndexer implements a block indexer, indexing FinalizeBlock
// events with an underlying KV store. Block events are indexed by their height,
// such that matching search criteria returns the respective block height(s).
type BlockerIndexer struct {
	store dbm.DB
}

func New(store dbm.DB) *BlockerIndexer {
	return &BlockerIndexer{
		store: store,
	}
}

// Has returns true if the given height has been indexed. An error is returned
// upon database query failure.
func (idx *BlockerIndexer) Has(height int64) (bool, error) {
	key, err := heightKey(height)
	if err != nil {
		return false, fmt.Errorf("failed to create block height index key: %w", err)
	}

	return idx.store.Has(key)
}

// readWatermark returns the lowest block height covered by the height-ordered
// index. An unset watermark (fresh DB, or upgraded-but-not-yet-written) reads
// as math.MaxInt64 so every height-ordered query takes the fallback path
// until the new index has written at least one key.
func (idx *BlockerIndexer) readWatermark() (int64, error) {
	key, err := watermarkKey()
	if err != nil {
		return 0, err
	}
	bz, err := idx.store.Get(key)
	if err != nil {
		return 0, err
	}
	if len(bz) == 0 {
		return math.MaxInt64, nil
	}
	return int64FromBytes(bz), nil
}

// updateWatermark lowers the persisted watermark to height when height is lower,
// writing into the provided batch so it commits atomically with the
// height-ordered keys it accounts for. A watermark that is too high is only
// over-conservative (routes covered heights to the fallback).
func (idx *BlockerIndexer) updateWatermark(batch dbm.Batch, height int64) error {
	w, err := idx.readWatermark()
	if err != nil {
		return err
	}
	if height >= w {
		return nil
	}
	key, err := watermarkKey()
	if err != nil {
		return err
	}
	return batch.Set(key, int64ToBytes(height))
}

// Index indexes FinalizeBlock events for a given block by its height.
// The following is indexed:
//
// primary key: encode(block.height | height) => encode(height)
// FinalizeBlock events: encode(eventType.eventAttr|eventValue|height|finalize_block) => encode(height)
func (idx *BlockerIndexer) Index(bh types.EventDataNewBlockHeader) error {
	batch := idx.store.NewBatch()
	defer func() { _ = batch.Close() }()

	height := bh.Header.Height

	// 1. index by height
	key, err := heightKey(height)
	if err != nil {
		return fmt.Errorf("failed to create block height index key: %w", err)
	}
	if err := batch.Set(key, int64ToBytes(height)); err != nil {
		return err
	}

	// 2. index FinalizeBlock events
	if err := idx.indexEvents(batch, bh.ResultFinalizeBlock.Events, "finalize_block", height); err != nil {
		return fmt.Errorf("failed to index FinalizeBlock events: %w", err)
	}

	// 3. advance the height-ordered index watermark in the same atomic batch as
	// the keys it accounts for, so a crash can never leave the watermark below
	// the keys actually written.
	if err := idx.updateWatermark(batch, height); err != nil {
		return err
	}

	return batch.WriteSync()
}

// Search performs a query for block heights that match a given FinalizeBlock
// The given query can match against zero or more block heights. In the case
// of height queries, i.e. block.height=H, if the height is indexed, that height
// alone will be returned. An error and nil slice is returned. Otherwise, a
// non-nil slice and nil error is returned.
func (idx *BlockerIndexer) Search(ctx context.Context, q *query.Query, opts indexer.SearchOptions) ([]int64, error) {
	results := make([]int64, 0)
	if ctx.Err() != nil {
		return results, nil
	}

	conditions := q.Syntax()

	// Reject queries that reference the reserved height-ordered / watermark
	// prefixes as tags. Writes already reject these as event names, but the
	// scan paths build their prefix directly from the query tag, so an
	// unguarded EXISTS on one of them would iterate the entire reserved
	// namespace and behave as a no-op instead of returning no matches.
	for _, c := range conditions {
		if c.Tag == blockHeightOrderedKey || c.Tag == blockWatermarkKey {
			return nil, fmt.Errorf("tag %q is reserved and cannot be queried", c.Tag)
		}
	}

	// If there is an exact height query, return the result immediately
	// (if it exists).
	height, ok := lookForHeight(conditions)
	if ok {
		ok, err := idx.Has(height)
		if err != nil {
			return nil, err
		}

		if ok {
			return []int64{height}, nil
		}

		return results, nil
	}

	// Extract ranges. If both upper and lower bounds exist, it's better to get
	// them in order as to not iterate over kvs that are not within range.
	ranges, rangeIndexes := indexer.LookForRanges(conditions)

	// Fast path: when every condition can be driven by a single height-ordered
	// scan and point-probed, stream candidates in order_by order and stop at
	// the limit, so a broad query does not materialize and sort the full match
	// set. This uses the legacy index / primary key (full coverage) and ignores
	// the watermark for height order index.
	if plan, ok := planBounded(conditions, ranges, rangeIndexes); ok {
		return idx.searchBounded(ctx, plan, opts)
	}

	// Height-ordered path: an EXISTS-by-tag query has no equality to drive the
	// legacy fast path, but it is height-orderable. Drive it off the new
	// height-ordered index, splitting at the watermark so pre-upgrade heights
	// (not covered by the new index) fall back to the legacy index for full
	// coverage. Early-stops at opts.Limit.
	if plan, ok := planHeightOrdered(conditions, ranges, rangeIndexes); ok {
		return idx.searchHeightOrdered(ctx, plan, opts)
	}

	// Fallback: queries containing CONTAINS/MATCHES or non-height value ranges
	// cannot be driven by an in-order scan. Materialize the intersection as
	// before, then bound and order the result set. The scan is bounded by
	// opts.MaxScan and fails closed if the budget is exceeded.
	budget := indexer.NewScanBudget(opts.MaxScan)
	filteredHeights, err := idx.intersect(ctx, conditions, ranges, rangeIndexes, budget)
	if err != nil {
		return nil, err
	}

	return idx.collectBounded(ctx, filteredHeights, opts)
}

// intersect returns the set of height-encoded values that satisfy every
// condition (implicit AND). It seeds the set from the first condition's index
// matches, then intersects each remaining condition against it, so a height
// survives only if it matches all of them.
func (idx *BlockerIndexer) intersect(
	ctx context.Context,
	conditions []syntax.Condition,
	ranges indexer.QueryRanges,
	rangeIndexes []int,
	budget *indexer.ScanBudget,
) (map[string][]byte, error) {
	var heightsInitialized bool
	filteredHeights := make(map[string][]byte)

	// conditions to skip because they're handled before "everything else"
	skipIndexes := make([]int, 0)

	if len(ranges) > 0 {
		skipIndexes = append(skipIndexes, rangeIndexes...)

		for _, qr := range ranges {
			prefix, err := orderedcode.Append(nil, qr.Key)
			if err != nil {
				return nil, fmt.Errorf("failed to create prefix key: %w", err)
			}

			if !heightsInitialized {
				filteredHeights, err = idx.matchRange(ctx, qr, prefix, filteredHeights, true, budget)
				if err != nil {
					return nil, err
				}

				heightsInitialized = true

				// Ignore any remaining conditions if the first condition resulted in no
				// matches (assuming implicit AND operand).
				if len(filteredHeights) == 0 {
					break
				}
			} else {
				filteredHeights, err = idx.matchRange(ctx, qr, prefix, filteredHeights, false, budget)
				if err != nil {
					return nil, err
				}
			}
		}
	}

	// for all other conditions
	for i, c := range conditions {
		if intInSlice(i, skipIndexes) {
			continue
		}

		startKey, err := orderedcode.Append(nil, c.Tag, c.Arg.Value())
		if err != nil {
			return nil, err
		}

		if !heightsInitialized {
			filteredHeights, err = idx.match(ctx, c, startKey, filteredHeights, true, budget)
			if err != nil {
				return nil, err
			}

			heightsInitialized = true

			// Ignore any remaining conditions if the first condition resulted in no
			// matches (assuming implicit AND operand).
			if len(filteredHeights) == 0 {
				break
			}
		} else {
			filteredHeights, err = idx.match(ctx, c, startKey, filteredHeights, false, budget)
			if err != nil {
				return nil, err
			}
		}
	}

	return filteredHeights, nil
}

// collectBounded materializes filteredHeights into heights, orders them per
// opts.OrderDesc and truncates to opts.Limit. The intermediate match set is
// still fully materialized by intersect; only the returned slice and the sort
// cost are bounded here.
func (idx *BlockerIndexer) collectBounded(ctx context.Context, filteredHeights map[string][]byte, opts indexer.SearchOptions) ([]int64, error) {
	results := make([]int64, 0, len(filteredHeights))
	for _, hBz := range filteredHeights {
		h := int64FromBytes(hBz)

		ok, err := idx.Has(h)
		if err != nil {
			return nil, err
		}
		if ok {
			results = append(results, h)
		}

		if ctx.Err() != nil {
			break
		}
	}

	if opts.OrderDesc {
		sort.Slice(results, func(i, j int) bool { return results[i] > results[j] })
	} else {
		sort.Slice(results, func(i, j int) bool { return results[i] < results[j] })
	}

	if opts.Limit > 0 && len(results) > opts.Limit {
		results = results[:opts.Limit]
	}

	return results, nil
}

// collectBoundedInHeightRange is collectBounded restricted to heights within
// [lo, hi] and capped at limit. It backs the sub-watermark leg of the
// height-ordered split, where the legacy fallback must serve only pre-watermark
// heights.
func (idx *BlockerIndexer) collectBoundedInHeightRange(ctx context.Context, filteredHeights map[string][]byte, orderDesc bool, limit int, lo, hi int64) ([]int64, error) {
	results := make([]int64, 0, indexer.BoundedCap(limit))
	for _, hBz := range filteredHeights {
		h := int64FromBytes(hBz)
		if h < lo || h > hi {
			continue
		}

		ok, err := idx.Has(h)
		if err != nil {
			return nil, err
		}
		if ok {
			results = append(results, h)
		}

		if ctx.Err() != nil {
			break
		}
	}

	if orderDesc {
		sort.Slice(results, func(i, j int) bool { return results[i] > results[j] })
	} else {
		sort.Slice(results, func(i, j int) bool { return results[i] < results[j] })
	}

	if limit > 0 && len(results) > limit {
		results = results[:limit]
	}

	return results, nil
}

// searchBounded executes a boundedPlan: it scans the driver prefix in
// order_by order, point-probes the remaining conditions per candidate height,
// and stops as soon as opts.Limit matches are collected. Memory is bounded by
// the number of results kept rather than by the full match cardinality.
func (idx *BlockerIndexer) searchBounded(ctx context.Context, plan boundedPlan, opts indexer.SearchOptions) ([]int64, error) {
	var (
		prefix []byte
		err    error
	)
	if plan.driverEquality != nil {
		prefix, err = orderedcode.Append(nil, plan.driverEquality.Tag, plan.driverEquality.Arg.Value())
	} else {
		// Drive off the primary block.height key range.
		prefix, err = orderedcode.Append(nil, types.BlockHeightKey)
	}
	if err != nil {
		return nil, fmt.Errorf("failed to create driver prefix key: %w", err)
	}

	it, err := idx.prefixIterator(prefix, opts.OrderDesc)
	if err != nil {
		return nil, fmt.Errorf("failed to create driver iterator: %w", err)
	}
	defer func() { _ = it.Close() }()

	results := make([]int64, 0, indexer.BoundedCap(opts.Limit))
	seen := make(map[int64]struct{})

	for ; it.Valid(); it.Next() {
		if ctx.Err() != nil {
			break
		}

		h := int64FromBytes(it.Value())
		if _, dup := seen[h]; dup {
			continue
		}

		match, err := idx.candidateMatches(h, plan)
		if err != nil {
			return nil, err
		}
		if !match {
			continue
		}

		seen[h] = struct{}{}
		results = append(results, h)

		if opts.Limit > 0 && len(results) >= opts.Limit {
			break
		}
	}

	if err := it.Error(); err != nil {
		return nil, err
	}

	return results, nil
}

// candidateMatches reports whether the block at height h satisfies every
// non-driver condition in the plan: height-range bounds are evaluated directly
// from h, and equality probes are tested with a single point lookup against the
// event index.
func (idx *BlockerIndexer) candidateMatches(h int64, plan boundedPlan) (bool, error) {
	for i := range plan.heightRanges {
		if !indexer.HeightInRange(h, plan.heightRanges[i]) {
			return false, nil
		}
	}

	for i := range plan.equalityProbes {
		c := plan.equalityProbes[i]
		ok, err := idx.hasEvent(c.Tag, c.Arg.Value(), h)
		if err != nil {
			return false, err
		}
		if !ok {
			return false, nil
		}
	}

	// Confirm the height is indexed. Events and the height key are written in
	// the same atomic batch, so this is normally redundant, but it preserves
	// the historical guarantee and guards against partially written state.
	return idx.Has(h)
}

// prefixIterator returns an iterator over the given key prefix. When desc is
// true it iterates in descending (most-recent-height-first) order.
func (idx *BlockerIndexer) prefixIterator(prefix []byte, desc bool) (dbm.Iterator, error) {
	if !desc {
		return dbm.IteratePrefix(idx.store, prefix)
	}
	return idx.store.ReverseIterator(prefix, indexer.PrefixUpperBound(prefix))
}

// hasEvent reports whether the block at the given height has an indexed event
// matching compositeKey=eventValue, regardless of the event type suffix.
func (idx *BlockerIndexer) hasEvent(compositeKey, eventValue string, height int64) (bool, error) {
	prefix, err := orderedcode.Append(nil, compositeKey, eventValue, height)
	if err != nil {
		return false, err
	}

	it, err := dbm.IteratePrefix(idx.store, prefix)
	if err != nil {
		return false, err
	}
	defer func() { _ = it.Close() }()

	return it.Valid(), it.Error()
}

// boundedPlan describes a fast-path execution that bounds memory by driving a
// single height-ordered scan and point-probing the remaining conditions,
// rather than materializing and sorting the full match set.
type boundedPlan struct {
	// driverEquality, when non-nil, is the equality condition whose event-key
	// prefix is scanned in height order. When nil, the scan is driven off the
	// primary block.height key range instead.
	driverEquality *syntax.Condition
	// equalityProbes are the remaining equality conditions, tested per
	// candidate height with a point lookup.
	equalityProbes []syntax.Condition
	// heightRanges are block.height bounds evaluated directly from a candidate
	// height. When driving off the height range it is included here too so it
	// also filters candidates.
	heightRanges []indexer.QueryRange
}

// planBounded decides whether a query is eligible for the bounded fast path and
// builds its plan. A query qualifies only when every condition is either an
// equality (point-probeable) or a block.height range (evaluable from the
// candidate height), and there is at least one such condition to drive a
// height-ordered scan.
func planBounded(conditions []syntax.Condition, ranges indexer.QueryRanges, rangeIndexes []int) (boundedPlan, bool) {
	var plan boundedPlan

	// Every range must be a numeric block.height range; any other range needs
	// the attribute's value, which cannot be derived from the height alone.
	for key, qr := range ranges {
		if key != types.BlockHeightKey {
			return boundedPlan{}, false
		}
		if _, ok := qr.AnyBound().(int64); !ok {
			return boundedPlan{}, false
		}
		plan.heightRanges = append(plan.heightRanges, qr)
	}

	// Every non-range condition must be an equality to be point-probeable.
	var equalities []syntax.Condition
	for i, c := range conditions {
		if intInSlice(i, rangeIndexes) {
			continue
		}
		if c.Op != syntax.TEq {
			return boundedPlan{}, false
		}
		equalities = append(equalities, c)
	}

	switch {
	case len(equalities) > 0:
		// Drive off the first equality; its event-key prefix is height-ordered.
		plan.driverEquality = &equalities[0]
		plan.equalityProbes = equalities[1:]
	case len(plan.heightRanges) > 0:
		// No equality to drive off; searchBounded drives off the primary
		// block.height key range. heightRanges (populated above) already
		// filters candidates during the scan.
	default:
		// No sorted driver (e.g. an empty query); fall back.
		return boundedPlan{}, false
	}

	return plan, true
}

// heightOrderedPlan describes a query served from the height-ordered index: a
// single height-ordered scan of driverTag's prefix, filtered by any block.height
// bounds and split at the watermark (heights >= W come from the new index;
// heights < W come from the legacy fallback for full coverage).
type heightOrderedPlan struct {
	// driverTag is the composite key whose height-ordered prefix is scanned.
	driverTag string
	// heightRanges are block.height bounds; may be empty for a pure EXISTS query.
	heightRanges []indexer.QueryRange
	// existsCond is the single EXISTS condition this plan serves, retained so
	// the sub-watermark leg can rebuild the equivalent legacy match.
	existsCond *syntax.Condition
}

// planHeightOrdered decides whether a query is eligible for the height-ordered
// path and builds its plan. It handles a single EXISTS on a tag (optionally
// combined with a block.height range) — the height-orderable shape that has no
// equality to drive the legacy fast path and no primary-key coverage. A
// block.height range-only query is already served in height order by the
// primary key (see planBounded) and is not routed here.
func planHeightOrdered(conditions []syntax.Condition, ranges indexer.QueryRanges, rangeIndexes []int) (heightOrderedPlan, bool) {
	var plan heightOrderedPlan

	// Every range must be a numeric block.height range.
	for key, qr := range ranges {
		if key != types.BlockHeightKey {
			return heightOrderedPlan{}, false
		}
		if _, ok := qr.AnyBound().(int64); !ok {
			return heightOrderedPlan{}, false
		}
		plan.heightRanges = append(plan.heightRanges, qr)
	}

	// Collect the non-range conditions; exactly one EXISTS is required.
	nonRange := make([]syntax.Condition, 0, len(conditions))
	for i, c := range conditions {
		if intInSlice(i, rangeIndexes) {
			continue
		}
		nonRange = append(nonRange, c)
	}
	if len(nonRange) != 1 || nonRange[0].Op != syntax.TExists {
		return heightOrderedPlan{}, false
	}

	// block.height is never dual-written to the height-ordered namespace (it is
	// the reserved primary key), so it cannot drive a height-ordered scan. Fall
	// through to the intersect path, where EXISTS on the primary key correctly
	// resolves to the full set instead of scanning an empty prefix.
	if nonRange[0].Tag == types.BlockHeightKey {
		return heightOrderedPlan{}, false
	}

	plan.driverTag = nonRange[0].Tag
	plan.existsCond = &nonRange[0]
	return plan, true
}

// heightBounds reduces the plan's block.height ranges to a single inclusive
// [lo, hi] window. Heights are >= 1, so an absent lower bound defaults to 1
// (avoiding a pointless legacy scan for a non-existent sub-watermark tail).
func heightBounds(ranges []indexer.QueryRange) (lo, hi int64) {
	lo, hi = 1, int64(math.MaxInt64)
	for i := range ranges {
		if lb := ranges[i].LowerBoundValue(); lb != nil {
			if v, ok := lb.(int64); ok && v > lo {
				lo = v
			}
		}
		if ub := ranges[i].UpperBoundValue(); ub != nil {
			if v, ok := ub.(int64); ok && v < hi {
				hi = v
			}
		}
	}
	return lo, hi
}

// searchHeightOrdered serves a heightOrderedPlan by splitting the [lo, hi]
// window at the watermark W: heights in [max(lo, W), hi] are streamed from the
// new height-ordered index (fast, early-stops at opts.Limit); heights in
// [lo, W-1] are served by the legacy materializing fallback for full coverage.
// The two sub-ranges are height-disjoint at W, so no global merge is needed.
func (idx *BlockerIndexer) searchHeightOrdered(ctx context.Context, plan heightOrderedPlan, opts indexer.SearchOptions) ([]int64, error) {
	w, err := idx.readWatermark()
	if err != nil {
		return nil, err
	}
	lo, hi := heightBounds(plan.heightRanges)
	if lo > hi {
		return []int64{}, nil
	}

	fastLo := max(lo, w)
	hasFast := fastLo <= hi

	fbHi := min(hi, w-1)
	hasFallback := lo <= fbHi

	results := make([]int64, 0, indexer.BoundedCap(opts.Limit))
	remaining := func() int {
		if opts.Limit <= 0 {
			return -1
		}
		return opts.Limit - len(results)
	}
	reachedLimit := func() bool {
		return opts.Limit > 0 && len(results) >= opts.Limit
	}

	if opts.OrderDesc {
		if hasFast {
			r, err := idx.scanHeightOrderedFast(ctx, plan, fastLo, hi, true, remaining())
			if err != nil {
				return nil, err
			}
			results = append(results, r...)
		}
		if !reachedLimit() && hasFallback {
			r, err := idx.heightOrderedFallback(ctx, plan, lo, fbHi, true, remaining(), opts.MaxScan)
			if err != nil {
				return nil, err
			}
			results = append(results, r...)
		}
	} else {
		if hasFallback {
			r, err := idx.heightOrderedFallback(ctx, plan, lo, fbHi, false, remaining(), opts.MaxScan)
			if err != nil {
				return nil, err
			}
			results = append(results, r...)
		}
		if !reachedLimit() && hasFast {
			r, err := idx.scanHeightOrderedFast(ctx, plan, fastLo, hi, false, remaining())
			if err != nil {
				return nil, err
			}
			results = append(results, r...)
		}
	}

	if opts.Limit > 0 && len(results) > opts.Limit {
		results = results[:opts.Limit]
	}
	return results, nil
}

// scanHeightOrderedFast streams the new height-ordered index for plan.driverTag
// over heights [lo, hi] in order_by height order, deduping heights and stopping
// at limit (limit <= 0 means unbounded). Because keys are height-major the scan
// early-stops once it passes the far bound. This is a Limit-bounded fast-path
// scan and is deliberately not charged against the scan budget.
func (idx *BlockerIndexer) scanHeightOrderedFast(ctx context.Context, plan heightOrderedPlan, lo, hi int64, desc bool, limit int) ([]int64, error) {
	prefix, err := prefixHeightOrdered(plan.driverTag)
	if err != nil {
		return nil, err
	}
	it, err := idx.prefixIterator(prefix, desc)
	if err != nil {
		return nil, fmt.Errorf("failed to create height-ordered iterator: %w", err)
	}
	defer func() { _ = it.Close() }()

	results := make([]int64, 0, indexer.BoundedCap(limit))
	seen := make(map[int64]struct{})

	for ; it.Valid(); it.Next() {
		if ctx.Err() != nil {
			break
		}

		h, err := parseHeightFromHeightOrderedKey(it.Key())
		if err != nil {
			continue
		}

		// Keys are height-major, so once we pass the far bound in scan order we
		// can stop; the near bound is skipped until we reach the window.
		if desc {
			if h > hi {
				continue
			}
			if h < lo {
				break
			}
		} else {
			if h < lo {
				continue
			}
			if h > hi {
				break
			}
		}

		if _, dup := seen[h]; dup {
			continue
		}

		ok, err := idx.Has(h)
		if err != nil {
			return nil, err
		}
		if !ok {
			continue
		}

		seen[h] = struct{}{}
		results = append(results, h)

		if limit > 0 && len(results) >= limit {
			break
		}
	}

	if err := it.Error(); err != nil {
		return nil, err
	}

	return results, nil
}

// heightOrderedFallback serves the pre-watermark [lo, hi] leg of a
// heightOrderedPlan from the legacy index. It rebuilds the equivalent legacy
// EXISTS match, then collects heights in [lo, hi], ordered and capped at limit.
// The legacy scan is charged against a maxScan budget and fails closed if the
// budget is exceeded.
func (idx *BlockerIndexer) heightOrderedFallback(ctx context.Context, plan heightOrderedPlan, lo, hi int64, desc bool, limit, maxScan int) ([]int64, error) {
	budget := indexer.NewScanBudget(maxScan)

	filtered, err := idx.match(ctx, *plan.existsCond, nil, map[string][]byte{}, true, budget)
	if err != nil {
		return nil, err
	}

	return idx.collectBoundedInHeightRange(ctx, filtered, desc, limit, lo, hi)
}

// matchRange returns all matching block heights that match a given QueryRange
// and start key. An already filtered result (filteredHeights) is provided such
// that any non-intersecting matches are removed.
//
// NOTE: The provided filteredHeights may be empty if no previous condition has
// matched.
func (idx *BlockerIndexer) matchRange(
	ctx context.Context,
	qr indexer.QueryRange,
	startKey []byte,
	filteredHeights map[string][]byte,
	firstRun bool,
	budget *indexer.ScanBudget,
) (map[string][]byte, error) {

	// A previous match was attempted but resulted in no matches, so we return
	// no matches (assuming AND operand).
	if !firstRun && len(filteredHeights) == 0 {
		return filteredHeights, nil
	}

	tmpHeights := make(map[string][]byte)
	lowerBound := qr.LowerBoundValue()
	upperBound := qr.UpperBoundValue()

	it, err := dbm.IteratePrefix(idx.store, startKey)
	if err != nil {
		return nil, fmt.Errorf("failed to create prefix iterator: %w", err)
	}
	defer func() { _ = it.Close() }()

iter:
	for ; it.Valid(); it.Next() {
		if err := budget.Step(); err != nil {
			return nil, err
		}

		var (
			eventValue string
			err        error
		)

		if qr.Key == types.BlockHeightKey {
			eventValue, err = parseValueFromPrimaryKey(it.Key())
		} else {
			eventValue, err = parseValueFromEventKey(it.Key())
		}

		if err != nil {
			continue
		}

		if _, ok := qr.AnyBound().(int64); ok {
			v, err := strconv.ParseInt(eventValue, 10, 64)
			if err != nil {
				continue iter
			}

			include := true
			if lowerBound != nil && v < lowerBound.(int64) {
				include = false
			}

			if upperBound != nil && v > upperBound.(int64) {
				include = false
			}

			if include {
				tmpHeights[string(it.Value())] = it.Value()
			}
		}

		select {
		case <-ctx.Done():
			break iter

		default:
		}
	}

	if err := it.Error(); err != nil {
		return nil, err
	}

	if len(tmpHeights) == 0 || firstRun {
		// Either:
		//
		// 1. Regardless if a previous match was attempted, which may have had
		// results, but no match was found for the current condition, then we
		// return no matches (assuming AND operand).
		//
		// 2. A previous match was not attempted, so we return all results.
		return tmpHeights, nil
	}

	// Remove/reduce matches in filteredHashes that were not found in this
	// match (tmpHashes).
	for k := range filteredHeights {
		if tmpHeights[k] == nil {
			delete(filteredHeights, k)

			select {
			case <-ctx.Done():
				break

			default:
			}
		}
	}

	return filteredHeights, nil
}

// match returns all matching heights that meet a given query condition and start
// key. An already filtered result (filteredHeights) is provided such that any
// non-intersecting matches are removed.
//
// NOTE: The provided filteredHeights may be empty if no previous condition has
// matched.
func (idx *BlockerIndexer) match(
	ctx context.Context,
	c syntax.Condition,
	startKeyBz []byte,
	filteredHeights map[string][]byte,
	firstRun bool,
	budget *indexer.ScanBudget,
) (map[string][]byte, error) {

	// A previous match was attempted but resulted in no matches, so we return
	// no matches (assuming AND operand).
	if !firstRun && len(filteredHeights) == 0 {
		return filteredHeights, nil
	}

	tmpHeights := make(map[string][]byte)

	switch c.Op {
	case syntax.TEq:
		it, err := dbm.IteratePrefix(idx.store, startKeyBz)
		if err != nil {
			return nil, fmt.Errorf("failed to create prefix iterator: %w", err)
		}
		defer func() { _ = it.Close() }()

		for ; it.Valid(); it.Next() {
			if err := budget.Step(); err != nil {
				return nil, err
			}
			tmpHeights[string(it.Value())] = it.Value()

			if err := ctx.Err(); err != nil {
				break
			}
		}

		if err := it.Error(); err != nil {
			return nil, err
		}

	case syntax.TExists:
		prefix, err := orderedcode.Append(nil, c.Tag)
		if err != nil {
			return nil, err
		}

		it, err := dbm.IteratePrefix(idx.store, prefix)
		if err != nil {
			return nil, fmt.Errorf("failed to create prefix iterator: %w", err)
		}
		defer func() { _ = it.Close() }()

	iterExists:
		for ; it.Valid(); it.Next() {
			if err := budget.Step(); err != nil {
				return nil, err
			}
			tmpHeights[string(it.Value())] = it.Value()

			select {
			case <-ctx.Done():
				break iterExists

			default:
			}
		}

		if err := it.Error(); err != nil {
			return nil, err
		}

	case syntax.TContains:
		prefix, err := orderedcode.Append(nil, c.Tag)
		if err != nil {
			return nil, err
		}

		it, err := dbm.IteratePrefix(idx.store, prefix)
		if err != nil {
			return nil, fmt.Errorf("failed to create prefix iterator: %w", err)
		}
		defer func() { _ = it.Close() }()

	iterContains:
		for ; it.Valid(); it.Next() {
			if err := budget.Step(); err != nil {
				return nil, err
			}
			eventValue, err := parseValueFromEventKey(it.Key())
			if err != nil {
				continue
			}

			if strings.Contains(eventValue, c.Arg.Value()) {
				tmpHeights[string(it.Value())] = it.Value()
			}

			select {
			case <-ctx.Done():
				break iterContains

			default:
			}
		}
		if err := it.Error(); err != nil {
			return nil, err
		}

	case syntax.TMatches:
		prefix, err := orderedcode.Append(nil, c.Tag)
		if err != nil {
			return nil, err
		}

		it, err := dbm.IteratePrefix(idx.store, prefix)
		if err != nil {
			return nil, fmt.Errorf("failed to create prefix iterator: %w", err)
		}
		defer func() { _ = it.Close() }()

	iterMatches:
		for ; it.Valid(); it.Next() {
			if err := budget.Step(); err != nil {
				return nil, err
			}
			eventValue, err := parseValueFromEventKey(it.Key())
			if err != nil {
				continue
			}

			if match, _ := regexp.MatchString(c.Arg.Value(), eventValue); match {
				tmpHeights[string(it.Value())] = it.Value()
			}

			select {
			case <-ctx.Done():
				break iterMatches

			default:
			}
		}
		if err := it.Error(); err != nil {
			return nil, err
		}

	default:
		return nil, errors.New("other operators should be handled already")
	}

	if len(tmpHeights) == 0 || firstRun {
		// Either:
		//
		// 1. Regardless if a previous match was attempted, which may have had
		// results, but no match was found for the current condition, then we
		// return no matches (assuming AND operand).
		//
		// 2. A previous match was not attempted, so we return all results.
		return tmpHeights, nil
	}

	// Remove/reduce matches in filteredHeights that were not found in this
	// match (tmpHeights).
	for k := range filteredHeights {
		if tmpHeights[k] == nil {
			delete(filteredHeights, k)

			select {
			case <-ctx.Done():
				break

			default:
			}
		}
	}

	return filteredHeights, nil
}

func (idx *BlockerIndexer) indexEvents(batch dbm.Batch, events []abci.Event, typ string, height int64) error {
	heightBz := int64ToBytes(height)

	for _, event := range events {
		// only index events with a non-empty type
		if len(event.Type) == 0 {
			continue
		}

		for _, attr := range event.Attributes {
			if len(attr.Key) == 0 {
				continue
			}

			// index iff the event specified index:true and it's not a reserved event
			compositeKey := fmt.Sprintf("%s.%s", event.Type, string(attr.Key))
			if compositeKey == types.BlockHeightKey ||
				compositeKey == blockHeightOrderedKey || compositeKey == blockWatermarkKey {
				return fmt.Errorf("event type and attribute key \"%s\" is reserved; please use a different key", compositeKey)
			}

			if attr.GetIndex() {
				// dual-write: legacy value-ordered key plus the new
				// height-ordered key (see eventKeyHeightOrdered).
				key, err := eventKey(compositeKey, typ, string(attr.Value), height)
				if err != nil {
					return fmt.Errorf("failed to create block index key: %w", err)
				}
				if err := batch.Set(key, heightBz); err != nil {
					return err
				}

				hoKey, err := eventKeyHeightOrdered(compositeKey, typ, string(attr.Value), height)
				if err != nil {
					return fmt.Errorf("failed to create height-ordered block index key: %w", err)
				}
				if err := batch.Set(hoKey, heightBz); err != nil {
					return err
				}
			}
		}
	}

	return nil
}
