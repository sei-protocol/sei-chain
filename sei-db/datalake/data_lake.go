package datalake

import (
	"context"
	"io"
)

// DataLake is a durable store for the long-term or indefinite retention of values keyed by uint64. Reads may be
// slow. Unless the store allows gaps, its keys form one contiguous range.
//
// All methods are safe to call concurrently. Byte slices passed to or returned by the methods of a DataLake or its
// iterators are not safe to modify.
type DataLake interface {
	// Put writes value at key. It returns once the write is accepted, and the write becomes crash durable later.
	// key must be above every key in the store and every key deleted from it, and, in a store that does not allow
	// gaps and is not empty, one above the highest key. A completed rollback forgets every deletion above the
	// highest key it leaves in the store, or, if it leaves none, every deletion at or above its watermark.
	Put(
		ctx context.Context,
		key uint64,
		value []byte,
	) error

	// PutStream writes the bytes read from value at key. It returns once the write is accepted, and the write
	// becomes crash durable later. key must be above every key in the store and every key deleted from it, and, in
	// a store that does not allow gaps and is not empty, one above the highest key. A completed rollback forgets
	// every deletion above the highest key it leaves in the store, or, if it leaves none, every deletion at or
	// above its watermark. A failure of value is a background write failure.
	PutStream(
		ctx context.Context,
		key uint64,
		// Read in the background, possibly more than once: Reset() is called before each read after the first.
		// Closed by the store when it is done with it. Must not be used by the caller after the call.
		value DataLakeStream,
	) error

	// Get returns the value at key. A key below the durable watermark that has not been deleted is always
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

	// GetStream returns a reader over the value at key. A key below the durable watermark that has not been
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

	// Has reports whether key exists. A key below the durable watermark that has not been deleted is always
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

	// PruneBelow removes every key below key. key may not exceed the highest key. Until a later Flush() returns, a
	// crash may restore the removed keys.
	PruneBelow(
		ctx context.Context,
		// The lowest key to keep.
		key uint64,
	) error

	// GetDurableWatermark returns the durable watermark: every accepted write to a key strictly less than it is
	// crash durable.
	GetDurableWatermark() uint64

	// GetDeletionWatermark returns the deletion watermark: the lowest key whose object may still exist in the
	// backing store. Every deleted key below it is gone from the backing store.
	GetDeletionWatermark() uint64

	// GetFirstKey returns the lowest key in the store, whether or not it is crash durable yet.
	GetFirstKey() (
		key uint64,
		// False if the store is empty.
		ok bool,
	)

	// Flush blocks until every Put(), PutStream(), Delete() and PruneBelow() accepted before the call is crash
	// durable and every change to the backing store it requires is complete.
	Flush(ctx context.Context) error

	// Iterator returns an iterator, in key order, over the keys that are below the durable watermark when it
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

	// Close makes every accepted write crash durable, then releases the store's resources.
	Close() error
}
