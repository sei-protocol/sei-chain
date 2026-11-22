package cdkdatalake

import (
	"context"
	"errors"
	"fmt"
	"slices"

	"github.com/sei-protocol/sei-chain/sei-db/datalake"
)

var _ datalake.DataLakeIterator = (*storeIterator)(nil)

// storeIterator visits a sequence of a store's keys, reading their values from the bucket on request. With
// prefetching, it reads the values of the next keys in the background.
type storeIterator struct {
	// Reads values from the bucket.
	reader *bucketReader

	// Returns the next key to visit, and false once none remain.
	nextKey func(ctx context.Context) (uint64, bool, error)

	// The number of values fetched ahead of the current key, or 0 to fetch each value when it is requested.
	prefetchDepth int

	// Reads started for the keys after the current one, in visiting order.
	window []*getResult

	// The read of the current key's value, or nil if it has not been started.
	current *getResult

	// The current key.
	key uint64

	// True while the iterator is on a key.
	positioned bool
}

// newStoreIterator returns an iterator over the keys nextKey returns.
func newStoreIterator(
	reader *bucketReader,
	// Returns the next key to visit, and false once none remain.
	nextKey func(ctx context.Context) (uint64, bool, error),
	// The number of values to fetch ahead, or 0 for none.
	prefetchDepth int,
) *storeIterator {
	return &storeIterator{
		reader:        reader,
		nextKey:       nextKey,
		prefetchDepth: prefetchDepth,
	}
}

// keySequence returns a function yielding the keys in topology, from startKey in the direction of iteration. The
// topology must be complete.
func keySequence(
	topology keyTopology,
	startKey uint64,
	reverse bool,
) func() (uint64, bool) {
	if topology.AllowGaps() {
		keys := topology.Keys()
		index, found := slices.BinarySearch(keys, startKey)
		if reverse && !found {
			index--
		}
		return func() (uint64, bool) {
			if index < 0 || index >= len(keys) {
				return 0, false
			}
			key := keys[index]
			if reverse {
				index--
			} else {
				index++
			}
			return key, true
		}
	}

	first, last, ok := topology.Range()
	if !ok {
		return func() (uint64, bool) { return 0, false }
	}

	if reverse {
		next := min(startKey, last)
		exhausted := next < first
		return func() (uint64, bool) {
			if exhausted {
				return 0, false
			}
			key := next
			if key == first {
				exhausted = true
			} else {
				next--
			}
			return key, true
		}
	}

	next := max(startKey, first)
	exhausted := next > last
	return func() (uint64, bool) {
		if exhausted {
			return 0, false
		}
		key := next
		if key == last {
			exhausted = true
		} else {
			next++
		}
		return key, true
	}
}

// Next advances to the next key. See datalake.DataLakeIterator.
func (it *storeIterator) Next(ctx context.Context) (bool, error) {
	if err := ctx.Err(); err != nil {
		return false, fmt.Errorf("advance iterator: %w", err)
	}
	if it.prefetchDepth == 0 {
		key, ok, err := it.nextKey(ctx)
		if err != nil {
			return false, fmt.Errorf("advance iterator: %w", err)
		}
		it.key = key
		it.positioned = ok
		it.current = nil
		return ok, nil
	}

	for len(it.window) <= it.prefetchDepth {
		key, ok, err := it.nextKey(ctx)
		if err != nil {
			return false, fmt.Errorf("advance iterator: %w", err)
		}
		if !ok {
			break
		}
		it.window = append(it.window, it.reader.Get(ctx, key))
	}
	if len(it.window) == 0 {
		it.positioned = false
		it.current = nil
		return false, nil
	}
	it.current = it.window[0]
	it.window[0] = nil
	it.window = it.window[1:]
	it.key = it.current.Key()
	it.positioned = true
	return true, nil
}

// Key returns the current key. See datalake.DataLakeIterator.
func (it *storeIterator) Key() uint64 {
	return it.key
}

// Value returns the value at the current key. See datalake.DataLakeIterator.
func (it *storeIterator) Value(ctx context.Context) ([]byte, error) {
	if !it.positioned {
		return nil, errors.New("iterator is not on a key")
	}
	if it.current == nil {
		it.current = it.reader.Get(ctx, it.key)
	}
	value, found, err := it.current.Value(ctx)
	if err != nil {
		return nil, fmt.Errorf("read key %d: %w", it.key, err)
	}
	if !found {
		return nil, fmt.Errorf("key %d no longer exists", it.key)
	}
	return value, nil
}

// Close releases the iterator. See datalake.DataLakeIterator.
func (it *storeIterator) Close() error {
	return nil
}
