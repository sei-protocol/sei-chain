package query

import (
	"math"

	"github.com/sei-protocol/sei-chain/sei-cosmos/store/types"
	sdk "github.com/sei-protocol/sei-chain/sei-cosmos/types"
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
	req, err := preparePageRequest(pageRequest, scanLimit)
	if err != nil {
		return nil, err
	}

	if req.useKey {
		return runKeyPath(prefixStore, req, scanLimit, func(key, value []byte) (bool, error) {
			if err := onResult(key, value); err != nil {
				return false, err
			}
			return true, nil
		})
	}

	return runOffsetPathUnfiltered(prefixStore, req, scanLimit, onResult)
}
