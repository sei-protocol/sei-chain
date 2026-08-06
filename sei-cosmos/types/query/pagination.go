package query

import (
	"fmt"
	"math"

	"github.com/sei-protocol/sei-chain/sei-cosmos/store/types"
	db "github.com/tendermint/tm-db"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

// DefaultLimit is the default `limit` for queries
// if the `limit` is not supplied, paginate will use `DefaultLimit`
const DefaultLimit = 100

// DefaultMaxLimit is the default maximum limit per page the paginate
// function can handle, absent an operator override.
const DefaultMaxLimit = uint64(1_000)

// DefaultMaxScanLimit is the default maximum number of store entries the
// paginate function will iterate past the page end when count_total is
// requested, absent an operator override.
const DefaultMaxScanLimit = uint64(10_000)

// DefaultMaxOffset is the default maximum offset allowed in a PageRequest,
// absent an operator override.
const DefaultMaxOffset = uint64(10_000)

// MaxLimit, MaxScanLimit and MaxOffset are the pagination bounds enforced by
// ParsePagination, Paginate, FilteredPaginate and GenericFilteredPaginate.
// They start at their Default* values and are overridable at process
// startup via SetPaginationLimits (wired from the node's [pagination] app.toml
// section); nothing in this package mutates them afterwards.
var (
	MaxLimit     = DefaultMaxLimit
	MaxScanLimit = DefaultMaxScanLimit
	MaxOffset    = DefaultMaxOffset
)

// SetPaginationLimits overrides the package-level pagination bounds. It is
// intended to be called once, at node startup, from the resolved
// [pagination] app.toml configuration. A zero argument leaves the
// corresponding bound unchanged, so an app.toml that omits a key (or a caller
// that passes a partial override) cannot accidentally zero out a cap.
func SetPaginationLimits(maxLimit, maxOffset, maxScanLimit uint64) {
	if maxLimit != 0 {
		MaxLimit = maxLimit
	}
	if maxOffset != 0 {
		MaxOffset = maxOffset
	}
	if maxScanLimit != 0 {
		MaxScanLimit = maxScanLimit
	}
}

// ParsePagination validate PageRequest and returns page number & limit.
func ParsePagination(pageReq *PageRequest) (page, limit int, err error) {
	offset := 0
	limit = DefaultLimit

	if pageReq != nil {
		offset = int(pageReq.Offset) // #nosec G115 -- overflow checked below
		limit = int(pageReq.Limit)   // #nosec G115 -- overflow checked below
	}
	if offset < 0 {
		return 1, 0, status.Error(codes.InvalidArgument, "offset must greater than 0")
	}
	// #nosec G115 -- offset is non-negative after validation above; fits in uint64
	if offsetErr := VerifyPaginationOffset(uint64(offset)); offsetErr != nil {
		return 1, 0, offsetErr
	}

	if limit < 0 {
		return 1, 0, status.Error(codes.InvalidArgument, "limit must greater than 0")
	} else if limit == 0 {
		limit = DefaultLimit
	}

	// #nosec G115 -- limit is positive after validation above; fits in uint64
	if limitErr := VerifyPaginationLimit(uint64(limit)); limitErr != nil {
		return 1, 0, limitErr
	}

	page = offset/limit + 1

	return page, limit, nil
}

func VerifyPaginationLimit(limit uint64) error {
	if limit > MaxLimit {
		return status.Errorf(codes.InvalidArgument, "limit %d exceeds maximum allowed limit %d", limit, MaxLimit)
	}
	return nil
}

func VerifyPaginationOffset(offset uint64) error {
	if offset > MaxOffset {
		return status.Errorf(codes.InvalidArgument, "offset %d exceeds maximum allowed offset %d", offset, MaxOffset)
	}
	return nil
}

// Paginate does pagination of all the results in the PrefixStore based on the
// provided PageRequest. onResult should be used to do actual unmarshaling.
func Paginate(
	prefixStore types.KVStore,
	pageRequest *PageRequest,
	onResult func(key []byte, value []byte) error,
) (*PageResponse, error) {
	if pageRequest == nil {
		pageRequest = &PageRequest{}
	}
	offset := pageRequest.Offset
	key := pageRequest.Key
	limit := pageRequest.Limit
	if limit == 0 {
		limit = DefaultLimit
	}
	if offset > 0 && key != nil {
		return nil, fmt.Errorf("invalid request, either offset or key is expected, got both")
	}
	if err := VerifyPaginationLimit(limit); err != nil {
		return nil, err
	}
	if err := VerifyPaginationOffset(offset); err != nil {
		return nil, err
	}
	countTotal := pageRequest.CountTotal
	reverse := pageRequest.Reverse

	if len(key) != 0 {
		iterator := getIterator(prefixStore, key, reverse)
		defer func() { _ = iterator.Close() }()

		var count uint64
		var nextKey []byte

		for ; iterator.Valid(); iterator.Next() {
			if count == limit {
				nextKey = iterator.Key()
				break
			}
			if iterator.Error() != nil {
				return nil, iterator.Error()
			}
			err := onResult(iterator.Key(), iterator.Value())
			if err != nil {
				return nil, err
			}
			count++
		}

		return &PageResponse{
			NextKey: nextKey,
		}, nil
	}

	iterator := getIterator(prefixStore, nil, reverse)
	defer func() { _ = iterator.Close() }()

	end := paginationEnd(offset, limit)

	var count uint64
	var nextKey []byte

	for ; iterator.Valid(); iterator.Next() {
		count++

		if count > saturatingAdd(end, MaxScanLimit) {
			return nil, status.Errorf(codes.InvalidArgument,
				"scanned more than %d entries past the end of the page; use key-based pagination instead", MaxScanLimit)
		}

		if count <= offset {
			continue
		}
		if count <= end {
			err := onResult(iterator.Key(), iterator.Value())
			if err != nil {
				return nil, err
			}
		} else if count == saturatingAdd(end, 1) {
			nextKey = iterator.Key()

			if !countTotal {
				break
			}
		}
		if iterator.Error() != nil {
			return nil, iterator.Error()
		}
	}

	res := &PageResponse{NextKey: nextKey}
	if countTotal {
		res.Total = count
	}

	return res, nil
}

// paginationEnd returns offset+limit, saturating at math.MaxUint64 instead of
// wrapping. offset and limit are independently bounded by MaxOffset and
// MaxLimit in the callers below, but an operator can raise those bounds via
// SetPaginationLimits, so the sum can no longer be assumed to fit in a
// uint64 without this guard.
func paginationEnd(offset, limit uint64) uint64 {
	return saturatingAdd(offset, limit)
}

// saturatingAdd returns a+b, saturating at math.MaxUint64 instead of
// wrapping. Every scan-cap comparison below adds MaxScanLimit (or 1) to a
// value derived from offset/limit, both of which an operator can raise via
// SetPaginationLimits, so none of those sums can be assumed to fit in a
// uint64 without this guard either.
func saturatingAdd(a, b uint64) uint64 {
	if b > math.MaxUint64-a {
		return math.MaxUint64
	}
	return a + b
}

func getIterator(prefixStore types.KVStore, start []byte, reverse bool) db.Iterator {
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
