package query

import (
	"fmt"
	"math"

	"github.com/sei-protocol/sei-chain/sei-cosmos/store/types"
	sdk "github.com/sei-protocol/sei-chain/sei-cosmos/types"
	db "github.com/tendermint/tm-db"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

// DefaultLimit is the default `limit` for queries
// if the `limit` is not supplied, paginate will use `DefaultLimit`
const DefaultLimit = 100

// MaxLimit is the maximum limit the paginate function can handle
// which equals the maximum value that can be stored in uint64
const MaxLimit = uint64(math.MaxUint64)

// MaxScanLimit is the scan cap for untrusted ABCI query origins and the frozen
// consensus-path limit used by the v6.6-compatible paginators.
const MaxScanLimit = uint64(10_000)

// ParsePagination validate PageRequest and returns page number & limit.
// Note that page math is performed in int space, so limits and offsets above
// math.MaxInt64 are rejected as negative values.
func ParsePagination(pageReq *PageRequest) (page, limit int, err error) {
	offset := 0
	limit = DefaultLimit

	if pageReq != nil {
		offset = int(pageReq.Offset) // #nosec G115 -- negative on overflow, rejected below
		limit = int(pageReq.Limit)   // #nosec G115 -- negative on overflow, rejected below
	}
	if offset < 0 {
		return 1, 0, status.Error(codes.InvalidArgument, "offset must greater than 0")
	}

	if limit < 0 {
		return 1, 0, status.Error(codes.InvalidArgument, "limit must greater than 0")
	} else if limit == 0 {
		limit = DefaultLimit
	}

	page = offset/limit + 1

	return page, limit, nil
}

// Paginate does pagination of all the results in the PrefixStore based on the
// provided PageRequest. onResult should be used to do actual unmarshaling.
func Paginate(
	ctx sdk.Context,
	prefixStore types.KVStore,
	pageRequest *PageRequest,
	onResult func(key []byte, value []byte) error,
) (*PageResponse, error) {
	scanLimit := scanLimitParamsFromContext(ctx)

	// if the PageRequest is nil, use default PageRequest
	if pageRequest == nil {
		pageRequest = &PageRequest{}
	}

	offset := pageRequest.Offset
	key := pageRequest.Key
	limit := pageRequest.Limit
	countTotal := pageRequest.CountTotal
	reverse := pageRequest.Reverse

	if offset > 0 && key != nil {
		return nil, fmt.Errorf("invalid request, either offset or key is expected, got both")
	}

	// Note: unlike upstream cosmos-sdk, limit == 0 must NOT implicitly enable
	// countTotal. EVM precompiles (e.g. precompiles/staking) call query
	// handlers during transaction execution with Limit: 0; an implicit
	// full-store count would change their gas consumption and therefore
	// break AppHash and LastResultsHash across versions.
	if limit == 0 {
		limit = DefaultLimit
	}

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

	var (
		count            uint64
		nextKey          []byte
		pageCompleteIter uint64
	)

	for ; iterator.Valid(); iterator.Next() {
		count++

		if scanLimit.enforce && count <= offset && count > scanLimit.limit {
			return nil, scanLimitError(scanLimit.limit, "use key-based pagination instead")
		}
		if count >= end {
			pageCompleteIter++
		}
		if err := scanLimit.checkPostPage(pageCompleteIter, countTotal); err != nil {
			return nil, err
		}

		if count <= offset {
			continue
		}
		if count <= end {
			err := onResult(iterator.Key(), iterator.Value())
			if err != nil {
				return nil, err
			}
		} else if count == end+1 {
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

// paginationEnd returns the index one past the last entry of the requested
// page, saturating at math.MaxUint64 instead of wrapping around when
// offset+limit overflows.
func paginationEnd(offset, limit uint64) uint64 {
	if limit > math.MaxUint64-offset {
		return math.MaxUint64
	}
	return offset + limit
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
