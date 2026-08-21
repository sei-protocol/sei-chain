package query

import (
	"fmt"
	"math"

	storetypes "github.com/sei-protocol/sei-chain/sei-cosmos/store/types"
	db "github.com/tendermint/tm-db"
)

// scanPhase describes where the offset paginator is in the store scan.
type scanPhase int

const (
	scanPhaseSkip scanPhase = iota
	scanPhaseCollect
	scanPhasePostPage
)

type pageRequestNorm struct {
	offset     uint64
	limit      uint64
	end        uint64
	countTotal bool
	reverse    bool
	startKey   []byte
	useKey     bool
}

func normalizePageRequest(pageRequest *PageRequest) (pageRequestNorm, error) {
	if pageRequest == nil {
		pageRequest = &PageRequest{}
	}

	if pageRequest.Offset > 0 && pageRequest.Key != nil {
		return pageRequestNorm{}, fmt.Errorf("invalid request, either offset or key is expected, got both")
	}

	limit := pageRequest.Limit
	// Note: unlike upstream cosmos-sdk, limit == 0 must NOT implicitly enable
	// countTotal. EVM precompiles call query handlers during transaction
	// execution with Limit: 0; an implicit full-store count would change gas
	// consumption and break AppHash and LastResultsHash across versions.
	if limit == 0 {
		limit = DefaultLimit
	}

	return pageRequestNorm{
		offset:     pageRequest.Offset,
		limit:      limit,
		end:        paginationEnd(pageRequest.Offset, limit),
		countTotal: pageRequest.CountTotal,
		reverse:    pageRequest.Reverse,
		startKey:   pageRequest.Key,
		useKey:     len(pageRequest.Key) != 0,
	}, nil
}

func preparePageRequest(pageRequest *PageRequest, scanLimit scanLimitParams) (pageRequestNorm, error) {
	req, err := normalizePageRequest(pageRequest)
	if err != nil {
		return pageRequestNorm{}, err
	}
	if err := scanLimit.checkRequest(req); err != nil {
		return pageRequestNorm{}, err
	}
	return req, nil
}

func paginationEnd(offset, limit uint64) uint64 {
	if limit > math.MaxUint64-offset {
		return math.MaxUint64
	}
	return offset + limit
}

func getIterator(prefixStore storetypes.KVStore, start []byte, reverse bool) db.Iterator {
	if reverse {
		var end []byte
		if start != nil {
			itr := prefixStore.Iterator(start, nil)
			defer func() { _ = itr.Close() }()
			if itr.Valid() {
				itr.Next()
				end = itr.Key()
			}
		}
		return prefixStore.ReverseIterator(nil, end)
	}
	return prefixStore.Iterator(start, nil)
}

func runKeyPath(
	store storetypes.KVStore,
	req pageRequestNorm,
	scanLimit scanLimitParams,
	accept func(key, value []byte) (countsTowardLimit bool, err error),
) (*PageResponse, error) {
	iterator := getIterator(store, req.startKey, req.reverse)
	defer func() { _ = iterator.Close() }()

	var (
		numHits   uint64
		nextKey   []byte
		totalIter uint64
	)

	for ; iterator.Valid(); iterator.Next() {
		if numHits == req.limit {
			nextKey = iterator.Key()
			break
		}

		if scanLimit.enforce {
			totalIter++
			if err := scanLimit.checkKeyPath(totalIter); err != nil {
				return nil, err
			}
		}

		if iterator.Error() != nil {
			return nil, iterator.Error()
		}

		counts, err := accept(iterator.Key(), iterator.Value())
		if err != nil {
			return nil, err
		}
		if counts {
			numHits++
		}
	}

	return &PageResponse{NextKey: nextKey}, nil
}

// offsetScanCursor tracks offset-pagination progress through the store.
// Unfiltered scans count every KV entry; filtered scans count only entries
// accepted by the caller's filter.
type offsetScanCursor struct {
	req       pageRequestNorm
	scanLimit scanLimitParams
	filtered  bool

	scanned          uint64
	hits             uint64
	nextKey          []byte
	pageCompleteIter uint64
}

func newOffsetScanCursor(req pageRequestNorm, scanLimit scanLimitParams, filtered bool) *offsetScanCursor {
	return &offsetScanCursor{
		req:       req,
		scanLimit: scanLimit,
		filtered:  filtered,
	}
}

func (c *offsetScanCursor) phase() scanPhase {
	if !c.filtered {
		switch {
		case c.scanned <= c.req.offset:
			return scanPhaseSkip
		case c.scanned <= c.req.end:
			return scanPhaseCollect
		default:
			return scanPhasePostPage
		}
	}

	switch {
	case c.hits < c.req.offset:
		return scanPhaseSkip
	case c.hits < c.req.end:
		return scanPhaseCollect
	default:
		return scanPhasePostPage
	}
}

func (c *offsetScanCursor) beginIteration() error {
	c.scanned++
	return c.checkScanBudgetBeforePageFilled()
}

// checkScanBudgetBeforePageFilled caps raw KV scans while the page is still
// being filled. Filtered and unfiltered offset paths share this guard.
func (c *offsetScanCursor) checkScanBudgetBeforePageFilled() error {
	if c.scanLimit.enforce && c.phase() != scanPhasePostPage &&
		c.scanned > paginationEnd(c.req.offset, c.scanLimit.limit) {
		return scanLimitError(c.scanLimit.limit, "use key-based pagination instead")
	}
	return nil
}

func (c *offsetScanCursor) checkPostPageBudget() (stop bool, err error) {
	if c.pageEndReached() {
		c.pageCompleteIter++
	}
	return c.scanLimit.checkPostPage(c.pageCompleteIter, c.req.countTotal)
}

// pageEndReached reports whether the current entry should count toward the
// post-page scan budget. Filtered scans use hit count; unfiltered scans enter
// post-page only after the in-page window is complete.
func (c *offsetScanCursor) pageEndReached() bool {
	return c.phase() == scanPhasePostPage
}

func (c *offsetScanCursor) accumulate() bool {
	return c.phase() == scanPhaseCollect
}

func (c *offsetScanCursor) recordFilteredHit(key []byte, matched bool) (stop bool) {
	if matched {
		c.hits++
	}
	if c.hits > c.req.end {
		if c.nextKey == nil {
			c.nextKey = key
		}
		return !c.req.countTotal
	}
	return false
}

func (c *offsetScanCursor) recordUnfilteredHit(key []byte) (stop bool) {
	if c.scanned == c.req.end+1 {
		c.nextKey = key
		return !c.req.countTotal
	}
	return false
}

func (c *offsetScanCursor) total() uint64 {
	if c.filtered {
		return c.hits
	}
	return c.scanned
}

func runOffsetPathUnfiltered(
	store storetypes.KVStore,
	req pageRequestNorm,
	scanLimit scanLimitParams,
	onResult func(key, value []byte) error,
) (*PageResponse, error) {
	iterator := getIterator(store, nil, req.reverse)
	defer func() { _ = iterator.Close() }()

	cursor := newOffsetScanCursor(req, scanLimit, false)

loop:
	for ; iterator.Valid(); iterator.Next() {
		if err := cursor.beginIteration(); err != nil {
			return nil, err
		}
		if stop, err := cursor.checkPostPageBudget(); err != nil {
			return nil, err
		} else if stop {
			break
		}

		switch cursor.phase() {
		case scanPhaseSkip:
			continue
		case scanPhaseCollect:
			if err := onResult(iterator.Key(), iterator.Value()); err != nil {
				return nil, err
			}
		case scanPhasePostPage:
			if cursor.recordUnfilteredHit(iterator.Key()) {
				break loop
			}
		}

		if iterator.Error() != nil {
			return nil, iterator.Error()
		}
	}

	res := &PageResponse{NextKey: cursor.nextKey}
	if req.countTotal {
		res.Total = cursor.total()
	}
	return res, nil
}

func runOffsetPathFiltered(
	store storetypes.KVStore,
	req pageRequestNorm,
	scanLimit scanLimitParams,
	onResult func(key, value []byte, accumulate bool) (hit bool, err error),
) (*PageResponse, error) {
	iterator := getIterator(store, nil, req.reverse)
	defer func() { _ = iterator.Close() }()

	cursor := newOffsetScanCursor(req, scanLimit, true)

	for ; iterator.Valid(); iterator.Next() {
		if err := cursor.beginIteration(); err != nil {
			return nil, err
		}
		if stop, err := cursor.checkPostPageBudget(); err != nil {
			return nil, err
		} else if stop {
			break
		}

		if iterator.Error() != nil {
			return nil, iterator.Error()
		}

		hit, err := onResult(iterator.Key(), iterator.Value(), cursor.accumulate())
		if err != nil {
			return nil, err
		}

		if cursor.recordFilteredHit(iterator.Key(), hit) {
			break
		}
	}

	res := &PageResponse{NextKey: cursor.nextKey}
	if req.countTotal {
		res.Total = cursor.total()
	}
	return res, nil
}
