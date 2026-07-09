package kv

import (
	"context"
	"encoding/hex"
	"fmt"
	"math"
	"regexp"
	"sort"
	"strconv"
	"strings"

	"github.com/gogo/protobuf/proto"
	"github.com/google/orderedcode"
	dbm "github.com/tendermint/tm-db"

	abci "github.com/sei-protocol/sei-chain/sei-tendermint/abci/types"
	"github.com/sei-protocol/sei-chain/sei-tendermint/internal/pubsub/query"
	"github.com/sei-protocol/sei-chain/sei-tendermint/internal/pubsub/query/syntax"
	indexer "github.com/sei-protocol/sei-chain/sei-tendermint/internal/state/indexer"
	"github.com/sei-protocol/sei-chain/sei-tendermint/types"
)

var _ indexer.TxIndexer = (*TxIndex)(nil)

const (
	// txHeightOrderedKey namespaces the height-ordered secondary index. Its
	// keys have the form orderedcode(txHeightOrderedKey, tag, height, index,
	// value), so within a tag they sort by (height, index) — matching the
	// (height, index) order tx_search returns — and stay disjoint from the
	// legacy value-ordered index that shares this store. It is reserved: events
	// may not use it as a composite key.
	txHeightOrderedKey = "tx.height_ordered"

	// txWatermarkKey stores the lowest block height covered by the
	// height-ordered index on this node (see readWatermark). It is reserved.
	txWatermarkKey = "tx.new_index_min_height"
)

// TxIndex is the simplest possible indexer
// It is backed by two kv stores:
// 1. txhash - result  (primary key)
// 2. event - txhash   (secondary key)
type TxIndex struct {
	store dbm.DB
}

// NewTxIndex creates new KV indexer.
func NewTxIndex(store dbm.DB) *TxIndex {
	return &TxIndex{
		store: store,
	}
}

// Get gets transaction from the TxIndex storage and returns it or nil if the
// transaction is not found.
func (txi *TxIndex) Get(hash []byte) (*abci.TxResultV2, error) {
	if len(hash) == 0 {
		return nil, indexer.ErrorEmptyHash
	}

	rawBytes, err := txi.store.Get(primaryKey(hash))
	if err != nil {
		panic(err)
	}
	if rawBytes == nil {
		return nil, nil
	}

	txResult := new(abci.TxResult)
	err = proto.Unmarshal(rawBytes, txResult)
	if err != nil {
		return nil, fmt.Errorf("error reading TxResult: %w", err)
	}

	return &abci.TxResultV2{Height: txResult.Height, Index: txResult.Index, Tx: txResult.Tx, Result: txResult.Result}, nil
}

// Index indexes transactions using the given list of events. Each key
// that indexed from the tx's events is a composite of the event type and the
// respective attribute's key delimited by a "." (eg. "account.number").
// Any event with an empty type is not indexed.
func (txi *TxIndex) Index(results []*abci.TxResultV2) error {
	b := txi.store.NewBatch()
	defer func() { _ = b.Close() }()

	minHeight := int64(math.MaxInt64)
	for _, result := range results {
		hash := types.Tx(result.Tx).Hash()
		hashBytes := hash[:]

		// index tx by events
		err := txi.indexEvents(result, hashBytes, b)
		if err != nil {
			return err
		}

		// index by height (always), in both the legacy value-ordered and the
		// new height-ordered index.
		err = b.Set(KeyFromHeight(result), hashBytes)
		if err != nil {
			return err
		}
		err = b.Set(keyFromHeightHeightOrdered(result), hashBytes)
		if err != nil {
			return err
		}

		rawBytes, err := proto.Marshal(&abci.TxResult{Height: result.Height, Index: result.Index, Tx: result.Tx, Result: result.Result})
		if err != nil {
			return err
		}
		// index by hash (always)
		err = b.Set(primaryKey(hashBytes), rawBytes)
		if err != nil {
			return err
		}

		if result.Height < minHeight {
			minHeight = result.Height
		}
	}

	// Advance the watermark for the height-ordered index in the same atomic
	// batch as the keys it accounts for, so a crash can never leave the
	// watermark below the keys actually written.
	if err := txi.updateWatermark(b, minHeight); err != nil {
		return err
	}

	return b.WriteSync()
}

func (txi *TxIndex) indexEvents(result *abci.TxResultV2, hash []byte, store dbm.Batch) error {
	for _, event := range result.Result.Events {
		// only index events with a non-empty type
		if len(event.Type) == 0 {
			continue
		}

		for _, attr := range event.Attributes {
			if len(attr.Key) == 0 {
				continue
			}

			// index if `index: true` is set
			compositeTag := fmt.Sprintf("%s.%s", event.Type, string(attr.Key))
			// ensure event does not conflict with a reserved prefix key
			if compositeTag == types.TxHashKey || compositeTag == types.TxHeightKey ||
				compositeTag == txHeightOrderedKey || compositeTag == txWatermarkKey {
				return fmt.Errorf("event type and attribute key \"%s\" is reserved; please use a different key", compositeTag)
			}
			if attr.GetIndex() {
				// dual-write: legacy value-ordered key plus the new
				// height-ordered key (see keyFromEventHeightOrdered).
				if err := store.Set(keyFromEvent(compositeTag, string(attr.Value), result), hash); err != nil {
					return err
				}
				if err := store.Set(keyFromEventHeightOrdered(compositeTag, string(attr.Value), result), hash); err != nil {
					return err
				}
			}
		}
	}

	return nil
}

// Search performs a search using the given query.
//
// It breaks the query into conditions (like "tx.height > 5"). For each
// condition, it queries the DB index. One special use cases here: (1) if
// "tx.hash" is found, it returns tx result for it (2) for range queries it is
// better for the client to provide both lower and upper bounds, so we are not
// performing a full scan.
//
// opts bounds and orders the result set (see indexer.SearchOptions): when a
// query is eligible for the bounded fast path it is streamed in (height, index)
// order_by order and capped at opts.Limit during the scan, so a broad query
// does not materialize and sort the full match set. Otherwise the intersection
// is materialized as before, then ordered and capped.
//
// Search will exit early and return any result fetched so far,
// when a message is received on the context chan.
func (txi *TxIndex) Search(ctx context.Context, q *query.Query, opts indexer.SearchOptions) ([]*abci.TxResultV2, error) {
	if ctx.Err() != nil {
		return make([]*abci.TxResultV2, 0), nil
	}

	// get a list of conditions (like "tx.height > 5")
	conditions := q.Syntax()

	// Reject queries that reference the reserved height-ordered / watermark
	// prefixes as tags. Writes already reject these as event names, but the
	// scan paths build their prefix directly from the query tag, so an
	// unguarded EXISTS on one of them would iterate the entire reserved
	// namespace and behave as a no-op instead of returning no matches.
	for _, c := range conditions {
		if c.Tag == txHeightOrderedKey || c.Tag == txWatermarkKey {
			return nil, fmt.Errorf("tag %q is reserved and cannot be queried", c.Tag)
		}
	}

	// if there is a hash condition, return the result immediately
	hash, ok, err := lookForHash(conditions)
	if err != nil {
		return nil, fmt.Errorf("error during searching for a hash in the query: %w", err)
	} else if ok {
		res, err := txi.Get(hash)
		switch {
		case err != nil:
			return []*abci.TxResultV2{}, fmt.Errorf("error while retrieving the result: %w", err)
		case res == nil:
			return []*abci.TxResultV2{}, nil
		default:
			return []*abci.TxResultV2{res}, nil
		}
	}

	// extract ranges
	// if both upper and lower bounds exist, it's better to get them in order not
	// no iterate over kvs that are not within range.
	ranges, rangeIndexes := indexer.LookForRanges(conditions)

	// Fast path: when every condition is an equality (point-probeable) or a
	// tx.height range (evaluable from the candidate height), drive a single
	// (height, index)-ordered scan in order_by order, point-probe the remaining
	// conditions per candidate, and stop at opts.Limit. This uses the legacy
	// index (equality has full coverage) and ignores the watermark. Memory is
	// bounded by the results kept, not by the total match cardinality.
	if plan, ok := planBounded(conditions, ranges, rangeIndexes); ok {
		return txi.searchBounded(ctx, plan, opts)
	}

	// Height-ordered path: tx.height range-only and EXISTS-by-tag queries have
	// no equality to drive the legacy fast path, but they are height-orderable.
	// Drive them off the new height-ordered index, splitting at the watermark so
	// pre-upgrade heights (not covered by the new index) fall back to the legacy
	// index for full coverage. Early-stops at opts.Limit.
	if plan, ok := planHeightOrdered(conditions, ranges, rangeIndexes); ok {
		return txi.searchHeightOrdered(ctx, plan, opts)
	}

	// Fallback: queries containing CONTAINS/MATCHES, non-height value ranges, or
	// a mix of those cannot be driven by an in-order scan. Materialize the
	// intersection as before, then bound and order the result set. The scan is
	// bounded by opts.MaxScan and fails closed if the budget is exceeded.
	budget := indexer.NewScanBudget(opts.MaxScan)
	filteredHashes, err := txi.intersect(ctx, conditions, ranges, rangeIndexes, budget)
	if err != nil {
		return nil, err
	}
	return txi.collectBounded(ctx, filteredHashes, opts)
}

// intersect returns the set of tx hashes that satisfy every condition (implicit
// AND). It seeds the set from the first condition's index matches, then
// intersects each remaining condition against it, so a tx survives only if it
// matches all of them. The returned map is keyed by tx hash string with the
// hash bytes as the value.
func (txi *TxIndex) intersect(
	ctx context.Context,
	conditions []syntax.Condition,
	ranges indexer.QueryRanges,
	rangeIndexes []int,
	budget *indexer.ScanBudget,
) (map[string][]byte, error) {
	var hashesInitialized bool
	filteredHashes := make(map[string][]byte)

	// conditions to skip because they're handled before "everything else"
	skipIndexes := make([]int, 0)

	if len(ranges) > 0 {
		skipIndexes = append(skipIndexes, rangeIndexes...)

		for _, qr := range ranges {
			var err error
			if !hashesInitialized {
				filteredHashes, err = txi.matchRange(ctx, qr, prefixFromCompositeKey(qr.Key), filteredHashes, true, budget)
				if err != nil {
					return nil, err
				}
				hashesInitialized = true

				// Ignore any remaining conditions if the first condition resulted
				// in no matches (assuming implicit AND operand).
				if len(filteredHashes) == 0 {
					return filteredHashes, nil
				}
			} else {
				filteredHashes, err = txi.matchRange(ctx, qr, prefixFromCompositeKey(qr.Key), filteredHashes, false, budget)
				if err != nil {
					return nil, err
				}
			}
		}
	}

	// if there is a height condition ("tx.height=3"), extract it
	height := lookForHeight(conditions)

	// for all other conditions
	for i, c := range conditions {
		if intInSlice(i, skipIndexes) {
			continue
		}

		var err error
		if !hashesInitialized {
			filteredHashes, err = txi.match(ctx, c, prefixForCondition(c, height), filteredHashes, true, budget)
			if err != nil {
				return nil, err
			}
			hashesInitialized = true

			// Ignore any remaining conditions if the first condition resulted
			// in no matches (assuming implicit AND operand).
			if len(filteredHashes) == 0 {
				return filteredHashes, nil
			}
		} else {
			filteredHashes, err = txi.match(ctx, c, prefixForCondition(c, height), filteredHashes, false, budget)
			if err != nil {
				return nil, err
			}
		}
	}

	return filteredHashes, nil
}

// collectBounded materializes filteredHashes into tx results, orders them by
// (height, index) per opts.OrderDesc and truncates to opts.Limit. The
// intermediate match set is still fully materialized by intersect; only the
// returned slice and the sort cost are bounded here.
func (txi *TxIndex) collectBounded(ctx context.Context, filteredHashes map[string][]byte, opts indexer.SearchOptions) ([]*abci.TxResultV2, error) {
	results := make([]*abci.TxResultV2, 0, len(filteredHashes))
	for _, h := range filteredHashes {
		res, err := txi.Get(h)
		if err != nil {
			return nil, fmt.Errorf("failed to get Tx{%X}: %w", h, err)
		}
		if res != nil {
			results = append(results, res)
		}

		// Potentially exit early.
		if ctx.Err() != nil {
			break
		}
	}

	sortResults(results, opts.OrderDesc)

	if opts.Limit > 0 && len(results) > opts.Limit {
		results = results[:opts.Limit]
	}

	return results, nil
}

// collectBoundedInHeightRange is collectBounded restricted to results whose
// height falls within [lo, hi] and capped at limit. It backs the sub-watermark
// leg of the height-ordered split, where the legacy fallback must serve only
// pre-watermark heights.
func (txi *TxIndex) collectBoundedInHeightRange(ctx context.Context, filteredHashes map[string][]byte, orderDesc bool, limit int, lo, hi int64) ([]*abci.TxResultV2, error) {
	results := make([]*abci.TxResultV2, 0, indexer.BoundedCap(limit))
	for _, h := range filteredHashes {
		res, err := txi.Get(h)
		if err != nil {
			return nil, fmt.Errorf("failed to get Tx{%X}: %w", h, err)
		}
		if res != nil && res.Height >= lo && res.Height <= hi {
			results = append(results, res)
		}

		// Potentially exit early.
		if ctx.Err() != nil {
			break
		}
	}

	sortResults(results, orderDesc)

	if limit > 0 && len(results) > limit {
		results = results[:limit]
	}

	return results, nil
}

// searchBounded executes a boundedPlan: it scans the driver equality's
// secondary-index prefix (which orders by (height, index)) in order_by order,
// point-probes the remaining conditions per candidate, and stops as soon as
// opts.Limit matches are collected. Memory is bounded by the number of results
// kept rather than by the full match cardinality.
func (txi *TxIndex) searchBounded(ctx context.Context, plan boundedPlan, opts indexer.SearchOptions) ([]*abci.TxResultV2, error) {
	prefix := prefixFromCompositeKeyAndValue(plan.driverEquality.Tag, plan.driverEquality.Arg.Value())

	it, err := txi.prefixIterator(prefix, opts.OrderDesc)
	if err != nil {
		return nil, fmt.Errorf("failed to create driver iterator: %w", err)
	}
	defer func() { _ = it.Close() }()

	results := make([]*abci.TxResultV2, 0, indexer.BoundedCap(opts.Limit))
	seen := make(map[string]struct{})

	for ; it.Valid(); it.Next() {
		if ctx.Err() != nil {
			break
		}

		hash := it.Value()
		if _, dup := seen[string(hash)]; dup {
			continue
		}

		height, index, err := parseHeightIndexFromKey(it.Key())
		if err != nil {
			continue
		}

		match, err := txi.candidateMatches(height, index, plan)
		if err != nil {
			return nil, err
		}
		if !match {
			continue
		}

		res, err := txi.Get(hash)
		if err != nil {
			return nil, fmt.Errorf("failed to get Tx{%X}: %w", hash, err)
		}
		if res == nil {
			continue
		}

		seen[string(hash)] = struct{}{}
		results = append(results, res)

		if opts.Limit > 0 && len(results) >= opts.Limit {
			break
		}
	}

	if err := it.Error(); err != nil {
		return nil, err
	}

	return results, nil
}

// candidateMatches reports whether the tx at (height, index) satisfies every
// non-driver condition in the plan: tx.height range bounds are evaluated
// directly from height, and equality probes are tested with a single point
// lookup against the event index.
func (txi *TxIndex) candidateMatches(height int64, index uint32, plan boundedPlan) (bool, error) {
	for i := range plan.heightRanges {
		if !indexer.HeightInRange(height, plan.heightRanges[i]) {
			return false, nil
		}
	}

	for i := range plan.equalityProbes {
		c := plan.equalityProbes[i]
		ok, err := txi.store.Has(secondaryKey(c.Tag, c.Arg.Value(), height, index))
		if err != nil {
			return false, err
		}
		if !ok {
			return false, nil
		}
	}

	return true, nil
}

// prefixIterator returns an iterator over the given key prefix. When desc is
// true it iterates in descending ((height, index) most-recent-first) order.
func (txi *TxIndex) prefixIterator(prefix []byte, desc bool) (dbm.Iterator, error) {
	if !desc {
		return dbm.IteratePrefix(txi.store, prefix)
	}
	return txi.store.ReverseIterator(prefix, indexer.PrefixUpperBound(prefix))
}

// boundedPlan describes a fast-path execution that bounds memory by driving a
// single (height, index)-ordered scan off one equality condition and
// point-probing the remaining conditions, rather than materializing and sorting
// the full match set.
type boundedPlan struct {
	// driverEquality is the equality condition whose secondary-index prefix
	// (tag, value) is scanned in (height, index) order.
	driverEquality *syntax.Condition
	// equalityProbes are the remaining equality conditions, tested per candidate
	// with a point lookup.
	equalityProbes []syntax.Condition
	// heightRanges are tx.height bounds evaluated directly from a candidate
	// height.
	heightRanges []indexer.QueryRange
}

// planBounded decides whether a query is eligible for the bounded fast path and
// builds its plan. A query qualifies only when every condition is either an
// equality (point-probeable) or a tx.height range (evaluable from the candidate
// height), and there is at least one equality to drive an ordered scan.
//
// A tx.height-range-only query does not qualify: the tx.height secondary index
// stores the height as a decimal string, so its key order is not numeric and it
// cannot drive an in-order scan.
func planBounded(conditions []syntax.Condition, ranges indexer.QueryRanges, rangeIndexes []int) (boundedPlan, bool) {
	var plan boundedPlan

	// Every range must be a numeric tx.height range; any other range needs the
	// attribute's value, which cannot be derived from the height alone.
	for key, qr := range ranges {
		if key != types.TxHeightKey {
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

	// Need at least one equality to drive an ordered scan.
	if len(equalities) == 0 {
		return boundedPlan{}, false
	}

	// Prefer a tx.height equality when one is present:
	// its (TxHeightKey, "N") prefix is partitioned to a single height, so the
	// scan visits only the txs in block N.
	driver := 0
	for i := range equalities {
		if equalities[i].Tag == types.TxHeightKey {
			driver = i
			break
		}
	}
	plan.driverEquality = &equalities[driver]
	plan.equalityProbes = make([]syntax.Condition, 0, len(equalities)-1)
	for i := range equalities {
		if i == driver {
			continue
		}
		plan.equalityProbes = append(plan.equalityProbes, equalities[i])
	}

	return plan, true
}

// heightOrderedPlan describes a query served from the height-ordered index: a
// single (height, index)-ordered scan of driverTag's prefix, filtered by any
// tx.height bounds and split at the watermark (heights >= W come from the new
// index; heights < W come from the legacy fallback for full coverage).
type heightOrderedPlan struct {
	// driverTag is the composite key whose height-ordered prefix is scanned.
	driverTag string
	// heightRanges are tx.height bounds; may be empty for a pure EXISTS query.
	heightRanges []indexer.QueryRange
	// existsCond, when non-nil, is the single EXISTS condition this plan serves
	// (nil for a tx.height-range-only query). It is retained so the
	// sub-watermark leg can rebuild the equivalent legacy match.
	existsCond *syntax.Condition
}

// planHeightOrdered decides whether a query is eligible for the height-ordered
// path and builds its plan. It handles the height-orderable shapes that have no
// equality to drive the legacy fast path: a tx.height range-only query, or a
// single EXISTS on a tag (optionally combined with a tx.height range). Anything
// else (CONTAINS/MATCHES, non-height ranges, multi-condition mixes) is left to
// the materializing fallback.
func planHeightOrdered(conditions []syntax.Condition, ranges indexer.QueryRanges, rangeIndexes []int) (heightOrderedPlan, bool) {
	var plan heightOrderedPlan

	// Every range must be a numeric tx.height range.
	for key, qr := range ranges {
		if key != types.TxHeightKey {
			return heightOrderedPlan{}, false
		}
		if _, ok := qr.AnyBound().(int64); !ok {
			return heightOrderedPlan{}, false
		}
		plan.heightRanges = append(plan.heightRanges, qr)
	}

	// Collect the non-range conditions.
	nonRange := make([]syntax.Condition, 0, len(conditions))
	for i, c := range conditions {
		if intInSlice(i, rangeIndexes) {
			continue
		}
		nonRange = append(nonRange, c)
	}

	switch {
	case len(nonRange) == 0:
		// tx.height range-only: need at least one height range to drive a scan.
		if len(plan.heightRanges) == 0 {
			return heightOrderedPlan{}, false
		}
		plan.driverTag = types.TxHeightKey
	case len(nonRange) == 1 && nonRange[0].Op == syntax.TExists:
		plan.driverTag = nonRange[0].Tag
		plan.existsCond = &nonRange[0]
	default:
		return heightOrderedPlan{}, false
	}

	return plan, true
}

// heightBounds reduces the plan's tx.height ranges to a single inclusive
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
// The two sub-ranges are height-disjoint at W, so no global merge is needed —
// whichever sub-range holds the higher (for desc) or lower (for asc) heights is
// drained first, and the other only if the limit is not yet met.
func (txi *TxIndex) searchHeightOrdered(ctx context.Context, plan heightOrderedPlan, opts indexer.SearchOptions) ([]*abci.TxResultV2, error) {
	w, err := txi.readWatermark()
	if err != nil {
		return nil, err
	}
	lo, hi := heightBounds(plan.heightRanges)
	if lo > hi {
		return []*abci.TxResultV2{}, nil
	}

	fastLo := max(lo, w)
	hasFast := fastLo <= hi

	fbHi := min(hi, w-1)
	hasFallback := lo <= fbHi

	// One scan budget is shared across both legs so MaxScan bounds the total
	// work of the query regardless of order_by, and so the height-ordered fast
	// leg is charged too — otherwise a query with the result cap disabled
	// (max-tx-search-results = 0) could stream an entire tag prefix unbounded.
	budget := indexer.NewScanBudget(opts.MaxScan)

	results := make([]*abci.TxResultV2, 0, indexer.BoundedCap(opts.Limit))
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
			r, err := txi.scanHeightOrderedFast(ctx, plan, fastLo, hi, true, remaining(), budget)
			if err != nil {
				return nil, err
			}
			results = append(results, r...)
		}
		if !reachedLimit() && hasFallback {
			r, err := txi.heightOrderedFallback(ctx, plan, lo, fbHi, true, remaining(), budget)
			if err != nil {
				return nil, err
			}
			results = append(results, r...)
		}
	} else {
		if hasFallback {
			r, err := txi.heightOrderedFallback(ctx, plan, lo, fbHi, false, remaining(), budget)
			if err != nil {
				return nil, err
			}
			results = append(results, r...)
		}
		if !reachedLimit() && hasFast {
			r, err := txi.scanHeightOrderedFast(ctx, plan, fastLo, hi, false, remaining(), budget)
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
// over heights [lo, hi] in (height, index) order_by order, deduping by hash and
// stopping at limit (limit <= 0 means unbounded). The iterator is seeked to the
// [lo, hi] key bounds, so out-of-window entries are never scanned; each examined
// entry is charged against the shared budget, which therefore counts only
// in-window work and fails closed on a broad query even when the result cap is
// disabled.
func (txi *TxIndex) scanHeightOrderedFast(ctx context.Context, plan heightOrderedPlan, lo, hi int64, desc bool, limit int, budget *indexer.ScanBudget) ([]*abci.TxResultV2, error) {
	start, end := heightOrderedBounds(plan.driverTag, lo, hi)
	var (
		it  dbm.Iterator
		err error
	)
	if desc {
		it, err = txi.store.ReverseIterator(start, end)
	} else {
		it, err = txi.store.Iterator(start, end)
	}
	if err != nil {
		return nil, fmt.Errorf("failed to create height-ordered iterator: %w", err)
	}
	defer func() { _ = it.Close() }()

	results := make([]*abci.TxResultV2, 0, indexer.BoundedCap(limit))
	seen := make(map[string]struct{})

	for ; it.Valid(); it.Next() {
		if ctx.Err() != nil {
			break
		}

		// The iterator is bounded to [lo, hi], so every entry is in-window; the
		// budget therefore only counts in-window work.
		if err := budget.Step(); err != nil {
			return nil, err
		}

		hash := it.Value()
		if _, dup := seen[string(hash)]; dup {
			continue
		}

		res, err := txi.Get(hash)
		if err != nil {
			return nil, fmt.Errorf("failed to get Tx{%X}: %w", hash, err)
		}
		if res == nil {
			continue
		}

		seen[string(hash)] = struct{}{}
		results = append(results, res)

		if limit > 0 && len(results) >= limit {
			break
		}
	}

	if err := it.Error(); err != nil {
		return nil, err
	}

	return results, nil
}

// heightOrderedFallback serves the pre-watermark [lo, hi] leg from the legacy
// index, ordered and capped at limit, charged against the shared budget.
//
// The legacy index is value-ordered, so heights aren't seekable: this scans the
// whole tag prefix (including >= W entries the fast leg already served) and
// discards out-of-range ones. Budget pressure from the discards is a
// transitional-window effect that a reindex removes.
func (txi *TxIndex) heightOrderedFallback(ctx context.Context, plan heightOrderedPlan, lo, hi int64, desc bool, limit int, budget *indexer.ScanBudget) ([]*abci.TxResultV2, error) {
	var (
		filtered map[string][]byte
		err      error
	)
	if plan.existsCond != nil {
		filtered, err = txi.match(ctx, *plan.existsCond, nil, map[string][]byte{}, true, budget)
	} else {
		qr := indexer.QueryRange{
			Key:               types.TxHeightKey,
			LowerBound:        lo,
			UpperBound:        hi,
			IncludeLowerBound: true,
			IncludeUpperBound: true,
		}
		filtered, err = txi.matchRange(ctx, qr, prefixFromCompositeKey(types.TxHeightKey), map[string][]byte{}, true, budget)
	}
	if err != nil {
		return nil, err
	}

	return txi.collectBoundedInHeightRange(ctx, filtered, desc, limit, lo, hi)
}

// sortResults orders tx results by (height, index): descending (most recent
// first) when desc is true, ascending otherwise. It matches the ordering the
// RPC layer applies after the search.
func sortResults(results []*abci.TxResultV2, desc bool) {
	if desc {
		sort.Slice(results, func(i, j int) bool {
			if results[i].Height == results[j].Height {
				return results[i].Index > results[j].Index
			}
			return results[i].Height > results[j].Height
		})
	} else {
		sort.Slice(results, func(i, j int) bool {
			if results[i].Height == results[j].Height {
				return results[i].Index < results[j].Index
			}
			return results[i].Height < results[j].Height
		})
	}
}

func lookForHash(conditions []syntax.Condition) (hash []byte, ok bool, err error) {
	for _, c := range conditions {
		if c.Tag == types.TxHashKey {
			decoded, err := hex.DecodeString(c.Arg.Value())
			return decoded, true, err
		}
	}
	return
}

// lookForHeight returns a height if there is an "height=X" condition.
func lookForHeight(conditions []syntax.Condition) (height int64) {
	for _, c := range conditions {
		if c.Tag == types.TxHeightKey && c.Op == syntax.TEq {
			return int64(c.Arg.Number())
		}
	}
	return 0
}

// match returns all matching txs by hash that meet a given condition and start
// key. An already filtered result (filteredHashes) is provided such that any
// non-intersecting matches are removed.
//
// NOTE: filteredHashes may be empty if no previous condition has matched.
func (txi *TxIndex) match(
	ctx context.Context,
	c syntax.Condition,
	startKeyBz []byte,
	filteredHashes map[string][]byte,
	firstRun bool,
	budget *indexer.ScanBudget,
) (map[string][]byte, error) {
	// A previous match was attempted but resulted in no matches, so we return
	// no matches (assuming AND operand).
	if !firstRun && len(filteredHashes) == 0 {
		return filteredHashes, nil
	}

	tmpHashes := make(map[string][]byte)

	switch c.Op {
	case syntax.TEq:
		it, err := dbm.IteratePrefix(txi.store, startKeyBz)
		if err != nil {
			return nil, err
		}
		defer func() { _ = it.Close() }()

	iterEqual:
		for ; it.Valid(); it.Next() {
			if err := budget.Step(); err != nil {
				return nil, err
			}
			tmpHashes[string(it.Value())] = it.Value()

			// Potentially exit early.
			select {
			case <-ctx.Done():
				break iterEqual
			default:
			}
		}
		if err := it.Error(); err != nil {
			return nil, err
		}

	case syntax.TExists:
		// XXX: can't use startKeyBz here because c.Operand is nil
		// (e.g. "account.owner/<nil>/" won't match w/ a single row)
		it, err := dbm.IteratePrefix(txi.store, prefixFromCompositeKey(c.Tag))
		if err != nil {
			return nil, err
		}
		defer func() { _ = it.Close() }()

	iterExists:
		for ; it.Valid(); it.Next() {
			if err := budget.Step(); err != nil {
				return nil, err
			}
			tmpHashes[string(it.Value())] = it.Value()

			// Potentially exit early.
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
		// XXX: startKey does not apply here.
		// For example, if startKey = "account.owner/an/" and search query = "account.owner CONTAINS an"
		// we can't iterate with prefix "account.owner/an/" because we might miss keys like "account.owner/Ulan/"
		it, err := dbm.IteratePrefix(txi.store, prefixFromCompositeKey(c.Tag))
		if err != nil {
			return nil, err
		}
		defer func() { _ = it.Close() }()

	iterContains:
		for ; it.Valid(); it.Next() {
			if err := budget.Step(); err != nil {
				return nil, err
			}
			value, err := parseValueFromKey(it.Key())
			if err != nil {
				continue
			}
			if strings.Contains(value, c.Arg.Value()) {
				tmpHashes[string(it.Value())] = it.Value()
			}

			// Potentially exit early.
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
		it, err := dbm.IteratePrefix(txi.store, prefixFromCompositeKey(c.Tag))
		if err != nil {
			return nil, err
		}
		defer func() { _ = it.Close() }()

	iterMatches:
		for ; it.Valid(); it.Next() {
			if err := budget.Step(); err != nil {
				return nil, err
			}
			value, err := parseValueFromKey(it.Key())
			if err != nil {
				continue
			}
			if match, _ := regexp.MatchString(c.Arg.Value(), value); match {
				tmpHashes[string(it.Value())] = it.Value()
			}

			// Potentially exit early.
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
		return nil, fmt.Errorf("other operators should be handled already")
	}

	if len(tmpHashes) == 0 || firstRun {
		// Either:
		//
		// 1. Regardless if a previous match was attempted, which may have had
		// results, but no match was found for the current condition, then we
		// return no matches (assuming AND operand).
		//
		// 2. A previous match was not attempted, so we return all results.
		return tmpHashes, nil
	}

	// Remove/reduce matches in filteredHashes that were not found in this
	// match (tmpHashes).
	for k := range filteredHashes {
		if tmpHashes[k] == nil {
			delete(filteredHashes, k)

			// Potentially exit early.
			select {
			case <-ctx.Done():
				break
			default:
			}
		}
	}

	return filteredHashes, nil
}

// matchRange returns all matching txs by hash that meet a given queryRange and
// start key. An already filtered result (filteredHashes) is provided such that
// any non-intersecting matches are removed.
//
// NOTE: filteredHashes may be empty if no previous condition has matched.
func (txi *TxIndex) matchRange(
	ctx context.Context,
	qr indexer.QueryRange,
	startKey []byte,
	filteredHashes map[string][]byte,
	firstRun bool,
	budget *indexer.ScanBudget,
) (map[string][]byte, error) {
	// A previous match was attempted but resulted in no matches, so we return
	// no matches (assuming AND operand).
	if !firstRun && len(filteredHashes) == 0 {
		return filteredHashes, nil
	}

	tmpHashes := make(map[string][]byte)
	lowerBound := qr.LowerBoundValue()
	upperBound := qr.UpperBoundValue()

	it, err := dbm.IteratePrefix(txi.store, startKey)
	if err != nil {
		return nil, err
	}
	defer func() { _ = it.Close() }()

iter:
	for ; it.Valid(); it.Next() {
		if err := budget.Step(); err != nil {
			return nil, err
		}
		value, err := parseValueFromKey(it.Key())
		if err != nil {
			continue
		}
		if _, ok := qr.AnyBound().(int64); ok {
			v, err := strconv.ParseInt(value, 10, 64)
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
				tmpHashes[string(it.Value())] = it.Value()
			}

			// XXX: passing time in a ABCI Events is not yet implemented
			// case time.Time:
			// 	v := strconv.ParseInt(extractValueFromKey(it.Key()), 10, 64)
			// 	if v == r.upperBound {
			// 		break
			// 	}
		}

		// Potentially exit early.
		select {
		case <-ctx.Done():
			break iter
		default:
		}
	}
	if err := it.Error(); err != nil {
		return nil, err
	}

	if len(tmpHashes) == 0 || firstRun {
		// Either:
		//
		// 1. Regardless if a previous match was attempted, which may have had
		// results, but no match was found for the current condition, then we
		// return no matches (assuming AND operand).
		//
		// 2. A previous match was not attempted, so we return all results.
		return tmpHashes, nil
	}

	// Remove/reduce matches in filteredHashes that were not found in this
	// match (tmpHashes).
	for k := range filteredHashes {
		if tmpHashes[k] == nil {
			delete(filteredHashes, k)

			// Potentially exit early.
			select {
			case <-ctx.Done():
				break
			default:
			}
		}
	}

	return filteredHashes, nil
}

// ##########################  Keys  #############################
//
// The indexer has two types of kv stores:
// 1. txhash - result  (primary key)
// 2. event - txhash   (secondary key)
//
// The event key can be decomposed into 4 parts.
// 1. A composite key which can be any string.
// Usually something like "tx.height" or "account.owner"
// 2. A value. That corresponds to the key. In the above
// example the value could be "5" or "Ivan"
// 3. The height of the Tx that aligns with the key and value.
// 4. The index of the Tx that aligns with the key and value

// the hash/primary key
func primaryKey(hash []byte) []byte {
	key, err := orderedcode.Append(
		nil,
		types.TxHashKey,
		string(hash),
	)
	if err != nil {
		panic(err)
	}
	return key
}

// The event/secondary key
func secondaryKey(compositeKey, value string, height int64, index uint32) []byte {
	key, err := orderedcode.Append(
		nil,
		compositeKey,
		value,
		height,
		int64(index),
	)
	if err != nil {
		panic(err)
	}
	return key
}

// parseValueFromKey parses an event key and extracts out the value, returning an error if one arises.
// This will also involve ensuring that the key has the correct format.
// CONTRACT: function doesn't check that the prefix is correct. This should have already been done by the iterator
func parseValueFromKey(key []byte) (string, error) {
	var (
		compositeKey, value string
		height, index       int64
	)
	remaining, err := orderedcode.Parse(string(key), &compositeKey, &value, &height, &index)
	if err != nil {
		return "", err
	}
	if len(remaining) != 0 {
		return "", fmt.Errorf("unexpected remainder in key: %s", remaining)
	}
	return value, nil
}

func keyFromEvent(compositeKey string, value string, result *abci.TxResultV2) []byte {
	return secondaryKey(compositeKey, value, result.Height, result.Index)
}

func KeyFromHeight(result *abci.TxResultV2) []byte {
	return secondaryKey(types.TxHeightKey, fmt.Sprintf("%d", result.Height), result.Height, result.Index)
}

// ##################  Height-ordered index (PLT-786)  ##################
//
// The height-ordered secondary key places the (real int64) height ahead of the
// value so that, within a composite tag, keys sort by (height, index) — the
// same order tx_search returns. This lets tx.height ranges and EXISTS-by-tag
// queries scan in result order and early-stop at the limit, instead of
// materializing and sorting the full match set. It is namespaced under the
// reserved txHeightOrderedKey so it stays disjoint from the legacy index in the
// shared store.

// secondaryKeyHeightOrdered builds a height-ordered event key:
// orderedcode(txHeightOrderedKey, compositeKey, height, index, value).
func secondaryKeyHeightOrdered(compositeKey, value string, height int64, index uint32) []byte {
	key, err := orderedcode.Append(
		nil,
		txHeightOrderedKey,
		compositeKey,
		height,
		int64(index),
		value,
	)
	if err != nil {
		panic(err)
	}
	return key
}

func keyFromEventHeightOrdered(compositeKey string, value string, result *abci.TxResultV2) []byte {
	return secondaryKeyHeightOrdered(compositeKey, value, result.Height, result.Index)
}

func keyFromHeightHeightOrdered(result *abci.TxResultV2) []byte {
	return secondaryKeyHeightOrdered(types.TxHeightKey, fmt.Sprintf("%d", result.Height), result.Height, result.Index)
}

// prefixHeightOrdered returns the scan prefix orderedcode(txHeightOrderedKey,
// compositeKey) covering every height-ordered entry for a composite tag, in
// (height, index) order.
func prefixHeightOrdered(compositeKey string) []byte {
	key, err := orderedcode.Append(nil, txHeightOrderedKey, compositeKey)
	if err != nil {
		panic(err)
	}
	return key
}

// heightOrderedBounds returns the [start, end) key range restricting a
// height-ordered scan of compositeKey to heights [lo, hi]. Because keys are
// orderedcode(txHeightOrderedKey, compositeKey, height, ...), start seeks to the
// first key at height lo and end is the first key past height hi, so the scan
// visits only in-window entries instead of scanning the whole prefix. When hi is
// unbounded (math.MaxInt64) end is the prefix upper bound, avoiding overflow.
func heightOrderedBounds(compositeKey string, lo, hi int64) (start, end []byte) {
	start, err := orderedcode.Append(nil, txHeightOrderedKey, compositeKey, lo)
	if err != nil {
		panic(err)
	}
	if hi == math.MaxInt64 {
		return start, indexer.PrefixUpperBound(prefixHeightOrdered(compositeKey))
	}
	end, err = orderedcode.Append(nil, txHeightOrderedKey, compositeKey, hi+1)
	if err != nil {
		panic(err)
	}
	return start, end
}

// watermarkKey is the reserved key holding the lowest height covered by the
// height-ordered index on this node.
func watermarkKey() []byte {
	key, err := orderedcode.Append(nil, txWatermarkKey)
	if err != nil {
		panic(err)
	}
	return key
}

// readWatermark returns the lowest block height covered by the height-ordered
// index. An unset watermark (fresh DB, or upgraded-but-not-yet-written) reads
// as math.MaxInt64 so every height-ordered query takes the legacy fallback
// until the new index has written at least one key.
func (txi *TxIndex) readWatermark() (int64, error) {
	bz, err := txi.store.Get(watermarkKey())
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
// over-conservative (routes covered heights to the fallback); a watermark below
// the keys actually written would be incorrect, so it is never raised here.
func (txi *TxIndex) updateWatermark(b dbm.Batch, height int64) error {
	w, err := txi.readWatermark()
	if err != nil {
		return err
	}
	if height >= w {
		return nil
	}
	return b.Set(watermarkKey(), int64ToBytes(height))
}

// Prefixes: these represent an initial part of the key and are used by iterators to iterate over a small
// section of the kv store during searches.

func prefixFromCompositeKey(compositeKey string) []byte {
	key, err := orderedcode.Append(nil, compositeKey)
	if err != nil {
		panic(err)
	}
	return key
}

func prefixFromCompositeKeyAndValue(compositeKey, value string) []byte {
	key, err := orderedcode.Append(nil, compositeKey, value)
	if err != nil {
		panic(err)
	}
	return key
}

// a small utility function for getting a keys prefix based on a condition and a height
func prefixForCondition(c syntax.Condition, height int64) []byte {
	key := prefixFromCompositeKeyAndValue(c.Tag, c.Arg.Value())
	if height > 0 {
		var err error
		key, err = orderedcode.Append(key, height)
		if err != nil {
			panic(err)
		}
	}
	return key
}
