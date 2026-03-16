package dbcache

import (
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
type Cache interface {

	// Get returns the value for the given key, or (nil, false, nil) if not found.
	// On a cache miss the provided Reader is called to fetch from the backing store,
	// and the result is loaded into the cache.
	//
	// It is not safe to mutate the key slice after calling this method, nor is it safe to mutate the value slice
	// that is returned.
	Get(
		// Reads a value from the backing store on cache miss.
		read Reader,
		// The entry to fetch.
		key []byte,
		// If true, the LRU queue will be updated. If false, the LRU queue will not be updated.
		// Useful for when an operation is performed multiple times in close succession on the same key,
		// since it requires non-zero overhead to do so with little benefit.
		updateLru bool,
	) ([]byte, bool, error)

	// Perform a batch read operation. Given a map of keys to read, performs the reads and updates the
	// map with the results. On cache misses the provided Reader is called to fetch from the backing store.
	//
	// It is not thread safe to read or mutate the map while this method is running. It is also not safe to mutate the
	// key or value slices in the map after calling this method.
	BatchGet(read Reader, keys map[string]types.BatchGetResult) error

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
