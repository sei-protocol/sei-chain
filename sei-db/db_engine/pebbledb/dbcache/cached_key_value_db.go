package dbcache

import (
	"fmt"

	errorutils "github.com/sei-protocol/sei-chain/sei-db/common/errors"
	"github.com/sei-protocol/sei-chain/sei-db/db_engine/types"
)

var _ types.KeyValueDB = (*cachedKeyValueDB)(nil)
var _ types.Checkpointable = (*cachedKeyValueDB)(nil)

type cachedKeyValueDB struct {
	db    types.KeyValueDB
	cache Cache
}

// Combine a cache and a key-value database to create a new key-value database with caching.
func NewCachedKeyValueDB(db types.KeyValueDB, cache Cache) types.KeyValueDB {
	return &cachedKeyValueDB{db: db, cache: cache}
}

func (c *cachedKeyValueDB) Get(key []byte) ([]byte, error) {
	val, found, err := c.cache.Get(key, true)
	if err != nil {
		return nil, fmt.Errorf("failed to get value from cache: %w", err)
	}
	if !found {
		return nil, errorutils.ErrNotFound
	}
	return val, nil
}

func (c *cachedKeyValueDB) BatchGet(keys map[string]types.BatchGetResult) error {
	err := c.cache.BatchGet(keys)
	if err != nil {
		return fmt.Errorf("failed to get values from cache: %w", err)
	}
	return nil
}

func (c *cachedKeyValueDB) Set(key []byte, value []byte, opts types.WriteOptions) error {
	err := c.db.Set(key, value, opts)
	if err != nil {
		return fmt.Errorf("failed to set value in database: %w", err)
	}
	c.cache.Set(key, value)
	return nil
}

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
