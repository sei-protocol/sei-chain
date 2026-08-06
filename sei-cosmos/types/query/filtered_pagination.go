package query

import (
	"fmt"

	"github.com/sei-protocol/sei-chain/sei-cosmos/codec"
	"github.com/sei-protocol/sei-chain/sei-cosmos/store/types"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

// FilteredPaginate does pagination of all the results in the PrefixStore based on the
// provided PageRequest. onResult does the unmarshaling and filtering.
// Key-based pagination is optimized; offset-based pagination lazily walks all records.
//
// Iteration is capped at MaxScanLimit raw entries per call to bound the work any single
// request can force. Hitting the cap before the page fills is not treated as failure: the
// call returns whatever hits it already found, plus a NextKey positioned where the scan
// stopped, so a client that keeps following NextKey makes bounded progress on a store where
// filtered matches are sparse. The one case that fails outright is count_total=true, since an
// exact Total cannot be honored once the scan is cut short — that combination returns an
// InvalidArgument error instead of a page with a misleading Total.
func FilteredPaginate(
	prefixStore types.KVStore,
	pageRequest *PageRequest,
	onResult func(key []byte, value []byte, accumulate bool) (bool, error),
) (*PageResponse, error) {

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

	if err := VerifyPaginationOffset(offset); err != nil {
		return nil, err
	}

	if limit == 0 {
		limit = DefaultLimit
	}

	if err := VerifyPaginationLimit(limit); err != nil {
		return nil, err
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
			if numHits == limit {
				nextKey = iterator.Key()
				break
			}
			totalIter++
			if totalIter > MaxScanLimit {
				// Scan budget exhausted before the page filled. Return the hits found
				// so far (possibly zero) with NextKey positioned here, rather than
				// discarding them behind an error — the key-based branch never
				// computes a Total, so there is no count_total exactness to protect.
				nextKey = iterator.Key()
				break
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
		// Phase 1: page not yet complete — cap raw iterations to prevent full-store
		// walks when the filter produces too few hits to fill the page.
		if numHits < end && totalIter > saturatingAdd(offset, MaxScanLimit) {
			if !countTotal && numHits >= offset {
				// The page may be partial (fewer than limit hits, possibly zero), but
				// those hits are real and already reported to onResult — hand them
				// back with a resumable key instead of discarding them behind an
				// error. A client that keeps following NextKey makes bounded
				// progress even on a store where matches are sparse.
				//
				// Only resume when the offset skip is complete.
				nextKey = iterator.Key()
				break
			}
			// count_total asked for an exact Total, which this call cannot produce
			// once the scan is cut short before the page even filled. When numHits
			// is still below offset the scan cap was hit before the skip finished,
			// so there is no faithful resume point either.
			return nil, status.Errorf(codes.InvalidArgument,
				"scanned more than %d entries without filling the page; retry without count_total, "+
					"or use key-based pagination", MaxScanLimit)
		}
		// Phase 2: page complete — cap how far past the page we scan for nextKey/count_total.
		if pageCompleteIter > MaxScanLimit {
			if !countTotal {
				// The page is already assembled. Whether another hit exists beyond
				// our scan window is unknown, so hand back a resumable key instead
				// of silently claiming this was the last page.
				nextKey = iterator.Key()
				break
			}
			return nil, status.Errorf(codes.InvalidArgument,
				"scanned more than %d entries past the end of the page; use key-based pagination instead", MaxScanLimit)
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

		if numHits == saturatingAdd(end, 1) {
			nextKey = iterator.Key()

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
//
// Scan limits: same semantics as FilteredPaginate — the scan is capped at MaxScanLimit raw
// entries per call, and hitting the cap before the page fills returns the results already
// found plus a resumable NextKey rather than an error, unless count_total was requested (see
// FilteredPaginate's documentation for why that combination cannot be honored).
func GenericFilteredPaginate[T codec.ProtoMarshaler, F codec.ProtoMarshaler](
	cdc codec.BinaryCodec,
	prefixStore types.KVStore,
	pageRequest *PageRequest,
	onResult func(key []byte, value T) (F, error),
	constructor func() T,
) ([]F, *PageResponse, error) {
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

	if err := VerifyPaginationOffset(offset); err != nil {
		return results, nil, err
	}

	if limit == 0 {
		limit = DefaultLimit
	}

	if err := VerifyPaginationLimit(limit); err != nil {
		return results, nil, err
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
			if numHits == limit {
				nextKey = iterator.Key()
				break
			}
			totalIter++
			if totalIter > MaxScanLimit {
				// Scan budget exhausted before the page filled. Return the results
				// found so far (possibly none) with NextKey positioned here, rather
				// than discarding them behind an error — the key-based branch never
				// computes a Total, so there is no count_total exactness to protect.
				nextKey = iterator.Key()
				break
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
		// Phase 1: page not yet complete — cap raw iterations to prevent full-store
		// walks when the filter produces too few hits to fill the page.
		if numHits < end && totalIter > saturatingAdd(offset, MaxScanLimit) {
			if !countTotal && numHits >= offset {
				// The page may be partial (fewer than limit results, possibly none),
				// but those results are real — hand them back with a resumable key
				// instead of discarding them behind an error. Only when the offset
				// skip is complete; see FilteredPaginate's Phase 1 guard.
				nextKey = iterator.Key()
				break
			}
			// count_total asked for an exact Total, which this call cannot produce
			// once the scan is cut short before the page even filled. When numHits
			// is still below offset the scan cap was hit before the skip finished,
			// so there is no faithful resume point either.
			return nil, nil, status.Errorf(codes.InvalidArgument,
				"scanned more than %d entries without filling the page; retry without count_total, "+
					"or use key-based pagination", MaxScanLimit)
		}
		// Phase 2: page complete — cap how far past the page we scan for nextKey/count_total.
		if pageCompleteIter > MaxScanLimit {
			if !countTotal {
				// The page is already assembled. Whether another hit exists beyond
				// our scan window is unknown, so hand back a resumable key instead
				// of silently claiming this was the last page.
				nextKey = iterator.Key()
				break
			}
			return nil, nil, status.Errorf(codes.InvalidArgument,
				"scanned more than %d entries past the end of the page; use key-based pagination instead", MaxScanLimit)
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

		if numHits == saturatingAdd(end, 1) {
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
