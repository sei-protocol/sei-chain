package query

import (
	"fmt"

	storetypes "github.com/sei-protocol/sei-chain/sei-cosmos/store/types"
	db "github.com/tendermint/tm-db"
)

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
	budget *iterationBudget,
	accept func(key, value []byte) (countsTowardLimit bool, err error),
) (*PageResponse, error) {
	iterator := getIterator(store, req.startKey, req.reverse)
	defer func() { _ = iterator.Close() }()

	var (
		numHits uint64
		nextKey []byte
	)

	for ; iterator.Valid(); iterator.Next() {
		if numHits == req.limit {
			nextKey = iterator.Key()
			break
		}

		if resumeKey, stop := budget.begin(iterator.Key()); stop {
			nextKey = resumeKey
			break
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

type offsetScanCursor struct {
	req      pageRequestNorm
	budget   *iterationBudget
	filtered bool

	scanned uint64
	hits    uint64
	nextKey []byte
}

func newOffsetScanCursor(req pageRequestNorm, budget *iterationBudget, filtered bool) *offsetScanCursor {
	return &offsetScanCursor{
		req:      req,
		budget:   budget,
		filtered: filtered,
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

func (c *offsetScanCursor) beginIteration(key []byte) (stop bool, err error) {
	if resumeKey, exhausted := c.budget.begin(key); exhausted {
		if c.phase() == scanPhaseSkip {
			return true, c.budget.offsetNotReachedError()
		}
		if c.nextKey == nil {
			c.nextKey = resumeKey
		}
		return true, nil
	}
	c.scanned++
	return false, nil
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
		return !c.req.countTotal || c.budget.omitTotal()
	}
	return false
}

func (c *offsetScanCursor) recordUnfilteredHit(key []byte) (stop bool) {
	if c.scanned == c.req.end+1 {
		c.nextKey = key
		return !c.req.countTotal || c.budget.omitTotal()
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
	budget *iterationBudget,
	onResult func(key, value []byte) error,
) (*PageResponse, error) {
	iterator := getIterator(store, nil, req.reverse)
	defer func() { _ = iterator.Close() }()

	cursor := newOffsetScanCursor(req, budget, false)

loop:
	for ; iterator.Valid(); iterator.Next() {
		if stop, err := cursor.beginIteration(iterator.Key()); err != nil {
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
	if req.countTotal && !budget.omitTotal() {
		res.Total = cursor.total()
	}
	return res, nil
}

func runOffsetPathFiltered(
	store storetypes.KVStore,
	req pageRequestNorm,
	budget *iterationBudget,
	onResult func(key, value []byte, accumulate bool) (hit bool, err error),
) (*PageResponse, error) {
	iterator := getIterator(store, nil, req.reverse)
	defer func() { _ = iterator.Close() }()

	cursor := newOffsetScanCursor(req, budget, true)

	for ; iterator.Valid(); iterator.Next() {
		if stop, err := cursor.beginIteration(iterator.Key()); err != nil {
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
	if req.countTotal && !budget.omitTotal() {
		res.Total = cursor.total()
	}
	return res, nil
}
