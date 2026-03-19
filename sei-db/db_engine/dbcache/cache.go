package dbcache

import (
	"context"
	"fmt"
	"time"

	"github.com/sei-protocol/sei-chain/sei-db/common/threading"
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

// DefaultEstimatedOverheadPerEntry is a rough estimate of the fixed heap overhead per cache entry
// on a 64-bit architecture (amd64/arm64). It accounts for the shardEntry struct (48 B),
// list.Element (48 B), lruQueueEntry (32 B), two map-entry costs (~64 B), string allocation
// rounding (~16 B), and a margin for the duplicate key copy stored in the LRU. Derived from
// static analysis of Go size classes and map bucket layout; validate experimentally for your
// target platform.
const DefaultEstimatedOverheadPerEntry uint64 = 250

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

// BuildCache creates a new Cache.
func BuildCache(
	ctx context.Context,
	shardCount uint64,
	maxSize uint64,
	readPool threading.Pool,
	miscPool threading.Pool,
	estimatedOverheadPerEntry uint64,
	cacheName string,
	metricsScrapeInterval time.Duration,
) (Cache, error) {

	if maxSize == 0 {
		return NewNoOpCache(), nil
	}

	cache, err := NewStandardCache(
		ctx,
		shardCount,
		maxSize,
		readPool,
		miscPool,
		estimatedOverheadPerEntry,
		cacheName,
		metricsScrapeInterval,
	)
	if err != nil {
		return nil, fmt.Errorf("failed to create cache: %w", err)
	}
	return cache, nil
}
