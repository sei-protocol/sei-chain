package dbcache

import (
	"fmt"

	errorutils "github.com/sei-protocol/sei-chain/sei-db/common/errors"
	"github.com/sei-protocol/sei-chain/sei-db/db_engine/types"
)

var _ types.KeyValueDB = (*cachedKeyValueDB)(nil)
var _ types.Checkpointable = (*cachedKeyValueDB)(nil)

// A unified interface for a key-value database and its read-through cache.
type cachedKeyValueDB struct {
	db    types.KeyValueDB
	cache Cache
	read  Reader
}

// Combine a cache and a key-value database into a unified interface.
//
// Due to the nature of a Cache, it is not safe to mutate byte slices (keys or values) passed to or received from
// any of the methods on a cachedKeyValueDB after calling them.
func NewCachedKeyValueDB(db types.KeyValueDB, cache Cache) types.KeyValueDB {
	read := func(key []byte) ([]byte, bool, error) {
		val, err := db.Get(key)
		if err != nil {
			if errorutils.IsNotFound(err) {
				return nil, false, nil
			}
			return nil, false, err
		}
		return val, true, nil
	}
	return &cachedKeyValueDB{db: db, cache: cache, read: read}
}

// Get returns the value for the given key, or ErrNotFound if not found.
//
// It is not safe to mutate the key slice after calling this method, nor is it safe to mutate the value slice
// that is returned.
func (c *cachedKeyValueDB) Get(key []byte) ([]byte, error) {
	val, found, err := c.cache.Get(c.read, key, true)
	if err != nil {
		return nil, fmt.Errorf("failed to get value from cache: %w", err)
	}
	if !found {
		return nil, errorutils.ErrNotFound
	}
	return val, nil
}

// BatchGet performs a batch read operation. Given a map of keys to read, performs the reads and updates the
// map with the results. On cache misses the provided Reader is called to fetch from the backing store.
//
// It is not thread safe to read or mutate the map while this method is running. It is also not safe to mutate the
// key or value slices in the map after calling this method.
func (c *cachedKeyValueDB) BatchGet(keys map[string]types.BatchGetResult) error {
	err := c.cache.BatchGet(c.read, keys)
	if err != nil {
		return fmt.Errorf("failed to get values from cache: %w", err)
	}
	return nil
}

// Set sets the value for the given key.
//
// It is not safe to mutate the key or value slices after calling this method.
func (c *cachedKeyValueDB) Set(key []byte, value []byte, opts types.WriteOptions) error {
	err := c.db.Set(key, value, opts)
	if err != nil {
		return fmt.Errorf("failed to set value in database: %w", err)
	}
	c.cache.Set(key, value)
	return nil
}

// Delete deletes the value for the given key.
//
// It is not safe to mutate the key slice after calling this method.
func (c *cachedKeyValueDB) Delete(key []byte, opts types.WriteOptions) error {
	err := c.db.Delete(key, opts)
	if err != nil {
		return fmt.Errorf("failed to delete value in database: %w", err)
	}
	c.cache.Delete(key)
	return nil
}

func (c *cachedKeyValueDB) NewIter(opts *types.IterOptions) (types.KeyValueDBIterator, error) {
	return c.db.NewIter(opts)
}

// NewBatch returns a new batch for atomic writes.
//
// It is not safe to mutate the key/value slices passed to the batch once inserted. This remains true even
// after the batch is committed.
func (c *cachedKeyValueDB) NewBatch() types.Batch {
	return newCachedBatch(c.db.NewBatch(), c.cache)
}

func (c *cachedKeyValueDB) Flush() error {
	return c.db.Flush()
}

func (c *cachedKeyValueDB) Close() error {
	return c.db.Close()
}

func (c *cachedKeyValueDB) Checkpoint(destDir string) error {
	cp, ok := c.db.(types.Checkpointable)
	if !ok {
		return fmt.Errorf("underlying database does not support Checkpoint")
	}
	return cp.Checkpoint(destDir)
}
