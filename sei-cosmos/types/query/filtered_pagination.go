package query

import (
	"fmt"

	"github.com/sei-protocol/sei-chain/sei-cosmos/codec"
	"github.com/sei-protocol/sei-chain/sei-cosmos/store/types"
	sdk "github.com/sei-protocol/sei-chain/sei-cosmos/types"
)

// FilteredPaginate does pagination of all the results in the PrefixStore based on the
// provided PageRequest. onResult should be used to do actual unmarshaling and filter the results.
// If key is provided, the pagination uses the optimized querying.
// If offset is used, the pagination uses lazy filtering i.e., searches through all the records.
// The accumulate parameter represents if the response is valid based on the offset given.
// It will be false for the results (filtered) < offset and true for offset <= accumulate < end.
// When accumulate is true, the current result should be appended to the result set returned
// to the client.
func FilteredPaginate(
	ctx sdk.Context,
	prefixStore types.KVStore,
	pageRequest *PageRequest,
	onResult func(key []byte, value []byte, accumulate bool) (bool, error),
) (*PageResponse, error) {
	return filteredPaginate(ctx, prefixStore, pageRequest, onResult, scanLimitParamsFromContext(ctx))
}

// FilteredPaginateForContext applies FilteredPaginate on the ABCI query path and
// FilteredPaginateV66 during consensus execution.
func FilteredPaginateForContext(
	ctx sdk.Context,
	prefixStore types.KVStore,
	pageRequest *PageRequest,
	onResult func(key []byte, value []byte, accumulate bool) (bool, error),
) (*PageResponse, error) {
	if ctx.IsABCIQuery() {
		return FilteredPaginate(ctx, prefixStore, pageRequest, onResult)
	}
	return FilteredPaginateV66(prefixStore, pageRequest, onResult)
}

// FilteredPaginateV66 preserves release/v6.6 behavior for EVM precompiles.
//
// Precompiles invoke Cosmos query handlers during transaction execution.
// Relaxing the historical scan limit there can change gas consumption and
// success/revert control flow, causing AppHash and LastResultsHash divergence.
// Node-local LCD/gRPC queries must continue using FilteredPaginate.
func FilteredPaginateV66(
	prefixStore types.KVStore,
	pageRequest *PageRequest,
	onResult func(key []byte, value []byte, accumulate bool) (bool, error),
) (*PageResponse, error) {
	return filteredPaginate(sdk.Context{}, prefixStore, pageRequest, onResult, scanLimitParams{enforce: true, limit: MaxScanLimit})
}

func filteredPaginate(
	ctx sdk.Context,
	prefixStore types.KVStore,
	pageRequest *PageRequest,
	onResult func(key []byte, value []byte, accumulate bool) (bool, error),
	scanLimit scanLimitParams,
) (*PageResponse, error) {
	_ = ctx
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
	// countTotal; see the note in Paginate.
	if limit == 0 {
		limit = DefaultLimit
	}

	if len(key) != 0 {
		iterator := getIterator(prefixStore, key, reverse)
		defer func() { _ = iterator.Close() }()

		var (
			numHits   uint64
			nextKey   []byte
			totalIter uint64
		)

		for ; iterator.Valid(); iterator.Next() {
			totalIter++
			if numHits == limit {
				nextKey = iterator.Key()
				break
			}
			if err := scanLimit.checkKeyPath(totalIter); err != nil {
				return nil, err
			}

			if iterator.Error() != nil {
				return nil, iterator.Error()
			}

			hit, err := onResult(iterator.Key(), iterator.Value(), true)
			if err != nil {
				return nil, err
			}

			if hit {
				numHits++
			}
		}

		return &PageResponse{
			NextKey: nextKey,
		}, nil
	}

	iterator := getIterator(prefixStore, nil, reverse)
	defer func() { _ = iterator.Close() }()

	end := paginationEnd(offset, limit)
	var (
		numHits          uint64
		nextKey          []byte
		totalIter        uint64
		pageCompleteIter uint64
	)

	for ; iterator.Valid(); iterator.Next() {
		totalIter++
		if err := scanLimit.checkOffsetBeforePageFilled(numHits, end, offset, totalIter); err != nil {
			return nil, err
		}
		if err := scanLimit.checkPostPage(pageCompleteIter, countTotal); err != nil {
			if !countTotal {
				break
			}
			return nil, err
		}

		if iterator.Error() != nil {
			return nil, iterator.Error()
		}

		accumulate := numHits >= offset && numHits < end
		hit, err := onResult(iterator.Key(), iterator.Value(), accumulate)
		if err != nil {
			return nil, err
		}

		if hit {
			numHits++
		}

		if numHits >= end {
			pageCompleteIter++
		}

		if numHits > end {
			// Only the first entry past the end of the page is the next key;
			// do not overwrite it while scanning the remainder for countTotal.
			if nextKey == nil {
				nextKey = iterator.Key()
			}

			if !countTotal {
				break
			}
		}
	}

	res := &PageResponse{NextKey: nextKey}
	if countTotal {
		res.Total = numHits
	}

	return res, nil
}

// GenericFilteredPaginate does pagination of all the results in the PrefixStore based on the
// provided PageRequest. `onResult` should be used to filter or transform the results.
// `c` is a constructor function that needs to return a new instance of the type T (this is to
// workaround some generic pitfalls in which we can't instantiate a T struct inside the function).
// If key is provided, the pagination uses the optimized querying.
// If offset is used, the pagination uses lazy filtering i.e., searches through all the records.
// The resulting slice (of type F) can be of a different type than the one being iterated through
// (type T), so it's possible to do any necessary transformation inside the onResult function.
func GenericFilteredPaginate[T codec.ProtoMarshaler, F codec.ProtoMarshaler](
	ctx sdk.Context,
	cdc codec.BinaryCodec,
	prefixStore types.KVStore,
	pageRequest *PageRequest,
	onResult func(key []byte, value T) (F, error),
	constructor func() T,
) ([]F, *PageResponse, error) {
	return genericFilteredPaginate(ctx, cdc, prefixStore, pageRequest, onResult, constructor, scanLimitParamsFromContext(ctx))
}

// GenericFilteredPaginateV66 preserves release/v6.6 behavior for EVM
// precompiles. Node-local LCD/gRPC queries must use GenericFilteredPaginate.
func GenericFilteredPaginateV66[T codec.ProtoMarshaler, F codec.ProtoMarshaler](
	cdc codec.BinaryCodec,
	prefixStore types.KVStore,
	pageRequest *PageRequest,
	onResult func(key []byte, value T) (F, error),
	constructor func() T,
) ([]F, *PageResponse, error) {
	return genericFilteredPaginate(sdk.Context{}, cdc, prefixStore, pageRequest, onResult, constructor, scanLimitParams{enforce: true, limit: MaxScanLimit})
}

func genericFilteredPaginate[T codec.ProtoMarshaler, F codec.ProtoMarshaler](
	ctx sdk.Context,
	cdc codec.BinaryCodec,
	prefixStore types.KVStore,
	pageRequest *PageRequest,
	onResult func(key []byte, value T) (F, error),
	constructor func() T,
	scanLimit scanLimitParams,
) ([]F, *PageResponse, error) {
	_ = ctx
	// if the PageRequest is nil, use default PageRequest
	if pageRequest == nil {
		pageRequest = &PageRequest{}
	}

	offset := pageRequest.Offset
	key := pageRequest.Key
	limit := pageRequest.Limit
	countTotal := pageRequest.CountTotal
	reverse := pageRequest.Reverse
	var results []F

	if offset > 0 && key != nil {
		return results, nil, fmt.Errorf("invalid request, either offset or key is expected, got both")
	}

	// Note: unlike upstream cosmos-sdk, limit == 0 must NOT implicitly enable
	// countTotal; see the note in Paginate.
	if limit == 0 {
		limit = DefaultLimit
	}

	if len(key) != 0 {
		iterator := getIterator(prefixStore, key, reverse)
		defer func() { _ = iterator.Close() }()

		var (
			numHits   uint64
			nextKey   []byte
			totalIter uint64
		)

		for ; iterator.Valid(); iterator.Next() {
			totalIter++
			if numHits == limit {
				nextKey = iterator.Key()
				break
			}
			if err := scanLimit.checkKeyPath(totalIter); err != nil {
				return nil, nil, err
			}

			if iterator.Error() != nil {
				return nil, nil, iterator.Error()
			}

			protoMsg := constructor()

			err := cdc.Unmarshal(iterator.Value(), protoMsg)
			if err != nil {
				return nil, nil, err
			}

			val, err := onResult(iterator.Key(), protoMsg)
			if err != nil {
				return nil, nil, err
			}

			if val.Size() != 0 {
				results = append(results, val)
				numHits++
			}
		}

		return results, &PageResponse{
			NextKey: nextKey,
		}, nil
	}

	iterator := getIterator(prefixStore, nil, reverse)
	defer func() { _ = iterator.Close() }()

	end := paginationEnd(offset, limit)
	var (
		numHits          uint64
		nextKey          []byte
		totalIter        uint64
		pageCompleteIter uint64
	)

	for ; iterator.Valid(); iterator.Next() {
		totalIter++
		if err := scanLimit.checkOffsetBeforePageFilled(numHits, end, offset, totalIter); err != nil {
			return nil, nil, err
		}
		if err := scanLimit.checkPostPage(pageCompleteIter, countTotal); err != nil {
			if !countTotal {
				break
			}
			return nil, nil, err
		}

		if iterator.Error() != nil {
			return nil, nil, iterator.Error()
		}

		protoMsg := constructor()

		err := cdc.Unmarshal(iterator.Value(), protoMsg)
		if err != nil {
			return nil, nil, err
		}

		val, err := onResult(iterator.Key(), protoMsg)
		if err != nil {
			return nil, nil, err
		}

		if val.Size() != 0 {
			// Previously this was the "accumulate" flag
			if numHits >= offset && numHits < end {
				results = append(results, val)
			}
			numHits++
		}

		if numHits >= end {
			pageCompleteIter++
		}

		if numHits > end {
			// Only the first entry past the end of the page is the next key;
			// do not overwrite it while scanning the remainder for countTotal.
			if nextKey == nil {
				nextKey = iterator.Key()
			}

			if !countTotal {
				break
			}
		}
	}

	res := &PageResponse{NextKey: nextKey}
	if countTotal {
		res.Total = numHits
	}

	return results, res, nil
}
