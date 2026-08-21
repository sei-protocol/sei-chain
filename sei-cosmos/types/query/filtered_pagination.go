package query

import (
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
	return filteredPaginate(prefixStore, pageRequest, onResult, scanLimitParamsFromContext(ctx))
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
	return filteredPaginate(prefixStore, pageRequest, onResult, v66ScanLimitParams())
}

func filteredPaginate(
	prefixStore types.KVStore,
	pageRequest *PageRequest,
	onResult func(key []byte, value []byte, accumulate bool) (bool, error),
	scanLimit scanLimitParams,
) (*PageResponse, error) {
	req, err := normalizePageRequest(pageRequest)
	if err != nil {
		return nil, err
	}

	if req.useKey {
		return runKeyPath(prefixStore, req, scanLimit, func(key, value []byte) (bool, error) {
			return onResult(key, value, true)
		})
	}

	return runOffsetPathFiltered(prefixStore, req, scanLimit, onResult)
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
	return genericFilteredPaginate(cdc, prefixStore, pageRequest, onResult, constructor, scanLimitParamsFromContext(ctx))
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
	return genericFilteredPaginate(cdc, prefixStore, pageRequest, onResult, constructor, v66ScanLimitParams())
}

func genericFilteredPaginate[T codec.ProtoMarshaler, F codec.ProtoMarshaler](
	cdc codec.BinaryCodec,
	prefixStore types.KVStore,
	pageRequest *PageRequest,
	onResult func(key []byte, value T) (F, error),
	constructor func() T,
	scanLimit scanLimitParams,
) ([]F, *PageResponse, error) {
	req, err := normalizePageRequest(pageRequest)
	if err != nil {
		return nil, nil, err
	}

	var results []F

	appendResult := func(key []byte, value []byte, accumulate bool) (bool, error) {
		protoMsg := constructor()
		if err := cdc.Unmarshal(value, protoMsg); err != nil {
			return false, err
		}

		val, err := onResult(key, protoMsg)
		if err != nil {
			return false, err
		}

		if val.Size() == 0 {
			return false, nil
		}

		if accumulate {
			results = append(results, val)
		}
		return true, nil
	}

	if req.useKey {
		pageRes, err := runKeyPath(prefixStore, req, scanLimit, func(key, value []byte) (bool, error) {
			hit, err := appendResult(key, value, true)
			return hit, err
		})
		return results, pageRes, err
	}

	pageRes, err := runOffsetPathFiltered(prefixStore, req, scanLimit, appendResult)
	return results, pageRes, err
}
