package dbcache

import (
	"context"
	"errors"
	"fmt"

	errorutils "github.com/sei-protocol/sei-chain/sei-db/common/errors"
	"github.com/sei-protocol/sei-chain/sei-db/db_engine/types"
)

// Reader reads a single key from the backing store.
//
// If the key does not exist, Reader must return (nil, false, nil) rather than an error.
// Errors are reserved for actual failures (e.g. I/O errors).
type Reader func(key []byte) (value []byte, found bool, err error)

// Cache describes a read-through cache backed by a Reader.
//
// Warning: it is not safe to mutate byte slices (keys or values) passed to or received from the cache.
// A cache is not required to make defensive copies, and so these slices must be treated as immutable.
//
// Although several methods on this interface return errors, the conditions when a cache
// is permitted to actually return an error is limited at the API level. A cache method
// may return an error under the following conditions:
// - malformed input (e.g. a nil key)
// - the Reader method returns an error (for methods that accpet a Reader)
// - the cache is shutting down
// - the cache's work pools are shutting down
//
// Cache errors are are generally not recoverable, and it should be assumed that a cache that has returned an error
// is in a corrupted state, and should be discarded.
type Cache interface {
	// Get returns the value for the given key, or (nil, false, nil) if not found.
	//
	// It is not safe to mutate the key slice after calling this method, nor is it safe to mutate the value slice
	// that is returned.
	Get(
		// The entry to fetch.
		key []byte,
		// If true, the LRU queue will be updated. If false, the LRU queue will not be updated.
		// Useful for when an operation is performed multiple times in close succession on the same key,
		// since it requires non-zero overhead to do so with little benefit.
		updateLru bool,
	) ([]byte, bool, error)

	// Perform a batch read operation. Given a map of keys to read, performs the reads and updates the
	// map with the results.
	//
	// It is not thread safe to read or mutate the map while this method is running. It is also not safe to mutate the
	// key or value slices in the map after calling this method.
	BatchGet(keys map[string]types.BatchGetResult) error

	// Set sets the value for the given key.
	//
	// It is not safe to mutate the key or value slices after calling this method.
	Set(key []byte, value []byte)

	// Delete deletes the value for the given key.
	//
	// It is not safe to mutate the key slice after calling this method.
	Delete(key []byte)

	// BatchSet applies the given updates to the cache.
	//
	// It is not safe to mutate the key or value slices in the CacheUpdate structs after calling this method.
	BatchSet(updates []CacheUpdate) error

	// Create a point-in-time snapshot of the data in the cache. This snapshot is thread safe to read, even
	// if concurrent operations are performed on this cache instance.
	//
	// Warning: it is NOT thread safe to read/write from the mutable cache (i.e. this object) concurrently with
	// the this method. It is, however, thread safe to interact with snapshots concurrently with
	// this method.
	Snapshot() (CacheSnapshot, error)

	// Close closes the cache and the underlying database. If this method returns a nil error, then all data
	// stored in snapshots will have been flushed persistantly to disk.
	//
	// It is not safe to call Close() concurrently with any other method on this interface, nor is it safe to call
	// Close() while any snapshots are still in use. It is legal to call Close() even if all snapshot reference
	// counts have not yet reached 0, but those snapshots are no longer safe to read when this method is called.
	Close() error
}

// A read-only snapshot of the data in the cache.
type CacheSnapshot interface {
	// Get returns the value for the given key, or (nil, false, nil) if not found.
	//
	// It is not safe to mutate the key slice after calling this method, nor is it safe to mutate the value slice
	// that is returned.
	Get(
		// The entry to fetch.
		key []byte,
		// If true, the LRU queue will be updated. If false, the LRU queue will not be updated.
		// Useful for when an operation is performed multiple times in close succession on the same key,
		// since it requires non-zero overhead to do so with little benefit.
		updateLru bool,
	) ([]byte, bool, error)

	// Perform a batch read operation. Given a map of keys to read, performs the reads and updates the
	// map with the results.
	//
	// It is not thread safe to read or mutate the map while this method is running. It is also not safe to mutate the
	// key or value slices in the map after calling this method.
	BatchGet(keys map[string]types.BatchGetResult) error

	// Get the diff contained within this snapshot, reletive to the previous snapshot.
	GetDiff() (map[string][]byte, error)

	// Acquire a reservation for the cache. While the reservation count exceeds 0, this snapshot is safe to read.
	// Once the reservation count reaches 0, the snapshot is no longer safe to read and its internal data
	// becomes eligible for cleanup.
	Reserve() error

	// Release a reservation for the cache. This must be called exactly once for each reservation acquired.
	// Note that when a snapshot is created, its reservation count is 1, meaning that this method should always
	// be called a number of times equal to the number of times Reserve() was called plus one.
	Release() error

	// SetHash attaches a hash to this snapshot. Must be called exactly once per snapshot
	// (except the boot snapshot, whose hash is auto-loaded from the DB). Returns an error
	// if hashing is disabled, hash is nil, or the hash has already been set.
	SetHash(hash []byte) error

	// GetHash returns the hash previously set via SetHash (or auto-loaded at boot).
	// Returns an error if the hash has not been set yet.
	GetHash() ([]byte, error)

	// AwaitHash blocks until the snapshot's hash becomes available, then returns it.
	// Returns an error immediately if hashing is disabled on this cache.
	AwaitHash(ctx context.Context) ([]byte, error)
}

// CacheUpdate describes a single key-value mutation to apply to the cache.
type CacheUpdate struct {
	// The key to update.
	Key []byte
	// The value to set. If nil, the key will be deleted.
	Value []byte
}

// IsDelete returns true if the update is a delete operation.
func (u *CacheUpdate) IsDelete() bool {
	return u.Value == nil
}

// ReaderFromDB constructs a Reader that reads from the given KeyValueDB.
func ReaderFromDB(db types.KeyValueDB) Reader {
	return func(key []byte) ([]byte, bool, error) {
		val, err := db.Get(key)
		if err != nil {
			if errors.Is(err, errorutils.ErrNotFound) {
				return nil, false, nil
			}
			return nil, false, fmt.Errorf("failed to read value from database: %w", err)
		}
		return val, true, nil
	}
}
