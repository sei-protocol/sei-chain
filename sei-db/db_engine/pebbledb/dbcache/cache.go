package dbcache

import (
	"context"
	"fmt"
	"time"

	"github.com/sei-protocol/sei-chain/sei-db/common/threading"
	"github.com/sei-protocol/sei-chain/sei-db/db_engine/types"
)

// Cache describes a cache capable of being used by a FlatKV store.
type Cache interface {

	// Get returns the value for the given key, or (nil, false) if not found.
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
	// It is not thread safe to read or mutate the map while this method is running.
	BatchGet(keys map[string]types.BatchGetResult) error

	// Set sets the value for the given key.
	Set(key []byte, value []byte)

	// Delete deletes the value for the given key.
	Delete(key []byte)

	// BatchSet applies the given updates to the cache.
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

// BuildCache creates a new Cache.
func BuildCache(
	ctx context.Context,
	readFunc func(key []byte) ([]byte, bool, error),
	shardCount uint64,
	maxSize uint64,
	readPool threading.Pool,
	miscPool threading.Pool,
	cacheName string,
	metricsScrapeInterval time.Duration,
) (Cache, error) {

	if maxSize == 0 {
		return NewNoOpCache(readFunc), nil
	}

	cache, err := NewStandardCache(
		ctx,
		readFunc,
		shardCount,
		maxSize,
		readPool,
		miscPool,
		cacheName,
		metricsScrapeInterval,
	)
	if err != nil {
		return nil, fmt.Errorf("failed to create cache: %w", err)
	}
	return cache, nil
}
