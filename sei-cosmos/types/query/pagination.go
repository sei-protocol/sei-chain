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

// MaxLimit is the maximum limit the paginate function can handle.
const MaxLimit = uint64(math.MaxUint64)

// MaxOffset is retained for compatibility. Pagination no longer applies an
// application-level offset cap.
const MaxOffset = uint64(math.MaxUint64)

// MaxScanLimit is retained for compatibility. Pagination no longer stops a
// valid request before the iterator is exhausted.
const MaxScanLimit = uint64(math.MaxUint64)

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
	if limit < 0 {
		return 1, 0, status.Error(codes.InvalidArgument, "limit must greater than 0")
	} else if limit == 0 {
		limit = DefaultLimit
	}

	page = offset/limit + 1

	return page, limit, nil
}

// VerifyPaginationLimit is retained for compatibility. Every uint64 limit is valid.
func VerifyPaginationLimit(uint64) error {
	return nil
}

// VerifyPaginationOffset is retained for compatibility. Every uint64 offset is valid.
func VerifyPaginationOffset(uint64) error {
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
	countTotal := pageRequest.CountTotal
	reverse := pageRequest.Reverse
	if limit == 0 {
		limit = DefaultLimit
	}
	if offset > 0 && key != nil {
		return nil, fmt.Errorf("invalid request, either offset or key is expected, got both")
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

	var count uint64
	var nextKey []byte

	for ; iterator.Valid(); iterator.Next() {
		count++

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
