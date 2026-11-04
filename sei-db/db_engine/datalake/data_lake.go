package datalake

import (
	"context"
	"io"
)

// DataLake is a durable store for the long-term or indefinite retention of values keyed by uint64. Reads may be
// slow. Unless the store allows gaps, its keys form one contiguous range.
//
// All methods are safe to call concurrently. Once a background write fails, every later call returns an error.
// Each ctx bounds how long its call may block. Byte slices passed to or returned by the methods of a DataLake or
// its iterators are not safe to modify.
type DataLake interface {
	// Put writes value at key. It returns once the write is accepted, and the write becomes crash durable later.
	// In a store that does not allow gaps, it returns an error if key is not one above the highest key; an empty
	// store accepts any key. In a store that allows gaps, writing a key that already exists is a background write
	// failure.
	Put(
		ctx context.Context,
		key uint64,
		value []byte,
	) error

	// PutStream writes the bytes read from value at key. It returns once the write is accepted, and the write
	// becomes crash durable later. In a store that does not allow gaps, it returns an error if key is not one above
	// the highest key; an empty store accepts any key. A failure to read value is a background write failure, as
	// is, in a store that allows gaps, writing a key that already exists.
	PutStream(
		ctx context.Context,
		key uint64,
		// Read and closed by the store in the background. Must not be used by the caller after the call.
		value io.ReadCloser,
	) error

	// Get returns the value at key. A key at or below the durable watermark that has not been deleted is always
	// found; a key above it may not be found yet.
	Get(
		ctx context.Context,
		key uint64,
	) (
		value []byte,
		// False if key was not found.
		ok bool,
		err error,
	)

	// GetStream returns a reader over the value at key. A key at or below the durable watermark that has not been
	// deleted is always found; a key above it may not be found yet.
	GetStream(
		ctx context.Context,
		key uint64,
	) (
		// Must be closed by the caller when ok is true.
		value io.ReadCloser,
		// False if key was not found.
		ok bool,
		err error,
	)

	// Has reports whether key exists. A key at or below the durable watermark that has not been deleted is always
	// found; a key above it may not be found yet.
	Has(
		ctx context.Context,
		key uint64,
	) (bool, error)

	// Delete removes key. The highest key may never be deleted. Beyond that, a store that allows gaps may delete
	// any key, and a store that does not may delete only the lowest key. Deleting an absent key does nothing. Until
	// a later Flush() returns, a crash may restore key.
	Delete(
		ctx context.Context,
		key uint64,
	) error

	// PruneBelow removes every key below key. It returns an error if that would remove the highest key. Until a
	// later Flush() returns, a crash may restore the removed keys.
	PruneBelow(
		ctx context.Context,
		// The lowest key to keep.
		key uint64,
	) error

	// GetDurableWatermark returns the key at or below which every accepted write is crash durable. When the store
	// opens, it is the highest key in the store.
	GetDurableWatermark() (
		watermark uint64,
		// False if nothing is crash durable.
		ok bool,
	)

	// GetFirstKey returns the lowest key in the store, whether or not it is crash durable yet.
	GetFirstKey() (
		key uint64,
		// False if the store is empty.
		ok bool,
	)

	// Flush blocks until every Put(), PutStream(), Delete() and PruneBelow() accepted before the call is crash
	// durable.
	Flush(ctx context.Context) error

	// Iterator returns an iterator, in key order, over the keys that are at or below the durable watermark when it
	// is called. The caller must close it.
	Iterator(
		ctx context.Context,
		// The first key visited if present, otherwise the next present key in the direction of iteration.
		startKey uint64,
		// If true, values are fetched in the background ahead of the iterator. If false, each value is fetched
		// when requested.
		prefetchValues bool,
		// If true, keys are visited in descending order, otherwise ascending.
		reverse bool,
	) (DataLakeIterator, error)

	// Close releases the store's resources. Writes that are not yet crash durable may be lost; call Flush() first
	// to keep them.
	Close() error
}
