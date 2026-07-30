package kv

import (
	"context"
	"errors"
	"fmt"
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

// BlockerIndexer implements a block indexer, indexing FinalizeBlock
// events with an underlying KV store. Block events are indexed by their height,
// such that matching search criteria returns the respective block height(s).
type BlockerIndexer struct {
	store dbm.DB
	// budget bounds the index entries in-flight searches may visit; nil means
	// unlimited. It is typically shared with the tx indexer so the cap is
	// process-wide across tx_search and block_search.
	budget *indexer.ScanBudget
}

func New(store dbm.DB) *BlockerIndexer {
	return &BlockerIndexer{
		store: store,
	}
}

// WithScanBudget attaches a shared scan budget that bounds how many index
// entries in-flight searches may visit.
func (idx *BlockerIndexer) WithScanBudget(budget *indexer.ScanBudget) *BlockerIndexer {
	idx.budget = budget
	return idx
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

	lease := idx.budget.Lease()
	defer lease.Release()

	conditions := q.Syntax()

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
	// set.
	if plan, ok := planBounded(conditions, ranges, rangeIndexes); ok {
		return idx.searchBounded(ctx, plan, opts, lease)
	}

	// Fallback: queries containing CONTAINS/MATCHES/EXISTS or non-height ranges
	// cannot be point-probed against a candidate height (the block's events
	// live only in the index, so there is nothing cheap to fetch). Materialize
	// the intersection as before, then bound and order the result set.
	filteredHeights, err := idx.intersect(ctx, conditions, ranges, rangeIndexes, lease)
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
	lease *indexer.ScanLease,
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
				filteredHeights, err = idx.matchRange(ctx, qr, prefix, filteredHeights, true, lease)
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
				filteredHeights, err = idx.matchRange(ctx, qr, prefix, filteredHeights, false, lease)
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
			filteredHeights, err = idx.match(ctx, c, startKey, filteredHeights, true, lease)
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
			filteredHeights, err = idx.match(ctx, c, startKey, filteredHeights, false, lease)
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

// searchBounded executes a boundedPlan: it scans the driver in order_by order,
// point-probes the remaining conditions per candidate height, and stops as soon
// as opts.Limit matches are collected. Memory is bounded by the number of
// results kept rather than by the full match cardinality.
func (idx *BlockerIndexer) searchBounded(ctx context.Context, plan boundedPlan, opts indexer.SearchOptions, lease *indexer.ScanLease) ([]int64, error) {
	it, err := idx.driverIterator(plan, opts.OrderDesc)
	if err != nil {
		return nil, err
	}
	defer func() { _ = it.Close() }()

	results := make([]int64, 0, indexer.BoundedCap(opts.Limit))
	seen := make(map[int64]struct{})

	for ; it.Valid(); it.Next() {
		if ctx.Err() != nil {
			break
		}

		// Charge each driver entry the scan walks against the shared budget.
		if err := lease.Visit(1); err != nil {
			return nil, err
		}

		h := int64FromBytes(it.Value())
		if _, dup := seen[h]; dup {
			continue
		}

		match, err := idx.candidateMatches(h, plan, lease)
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

// driverIterator builds the height-ordered iterator that searchBounded walks.
// An equality driver scans that condition's event-key prefix. A primary
// block.height range (no equality) is seeked to its inclusive [lower, upper]
// bounds so out-of-range heights are never walked or charged against the
// budget.
func (idx *BlockerIndexer) driverIterator(plan boundedPlan, desc bool) (dbm.Iterator, error) {
	if plan.driverEquality != nil {
		prefix, err := orderedcode.Append(nil, plan.driverEquality.Tag, plan.driverEquality.Arg.Value())
		if err != nil {
			return nil, fmt.Errorf("failed to create driver prefix key: %w", err)
		}
		return idx.prefixIterator(prefix, desc)
	}

	base, err := orderedcode.Append(nil, types.BlockHeightKey)
	if err != nil {
		return nil, fmt.Errorf("failed to create driver prefix key: %w", err)
	}

	start, end := base, indexer.PrefixUpperBound(base)
	qr := plan.heightRanges[0]
	if lb, ok := qr.LowerBoundValue().(int64); ok {
		if start, err = heightKey(lb); err != nil {
			return nil, fmt.Errorf("failed to create lower-bound key: %w", err)
		}
	}
	if ub, ok := qr.UpperBoundValue().(int64); ok {
		ubKey, err := heightKey(ub)
		if err != nil {
			return nil, fmt.Errorf("failed to create upper-bound key: %w", err)
		}
		end = indexer.PrefixUpperBound(ubKey)
	}

	if desc {
		return idx.store.ReverseIterator(start, end)
	}
	return idx.store.Iterator(start, end)
}

// candidateMatches reports whether the block at height h satisfies every
// non-driver condition in the plan: height-range bounds are evaluated directly
// from h, and equality probes are tested with a single point lookup against the
// event index. Each probe is charged against lease.
func (idx *BlockerIndexer) candidateMatches(h int64, plan boundedPlan, lease *indexer.ScanLease) (bool, error) {
	for i := range plan.heightRanges {
		if !indexer.HeightInRange(h, plan.heightRanges[i]) {
			return false, nil
		}
	}

	for i := range plan.equalityProbes {
		if err := lease.Visit(1); err != nil {
			return false, err
		}
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
	lease *indexer.ScanLease,
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
		if err := lease.Visit(1); err != nil {
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
	lease *indexer.ScanLease,
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
			if err := lease.Visit(1); err != nil {
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
			if err := lease.Visit(1); err != nil {
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
			if err := lease.Visit(1); err != nil {
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
			if err := lease.Visit(1); err != nil {
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
			if compositeKey == types.BlockHeightKey {
				return fmt.Errorf("event type and attribute key \"%s\" is reserved; please use a different key", compositeKey)
			}

			if attr.GetIndex() {
				key, err := eventKey(compositeKey, typ, string(attr.Value), height)
				if err != nil {
					return fmt.Errorf("failed to create block index key: %w", err)
				}

				if err := batch.Set(key, heightBz); err != nil {
					return err
				}
			}
		}
	}

	return nil
}
