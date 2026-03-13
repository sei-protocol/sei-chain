package dbcache

import (
	"fmt"

	"github.com/sei-protocol/sei-chain/sei-db/db_engine/types"
)

// cachedBatch wraps a types.Batch and applies pending mutations to the cache
// after a successful commit.
type cachedBatch struct {
	inner   types.Batch
	cache   Cache
	pending []CacheUpdate
}

var _ types.Batch = (*cachedBatch)(nil)

func newCachedBatch(inner types.Batch, cache Cache) *cachedBatch {
	return &cachedBatch{inner: inner, cache: cache}
}

func (cb *cachedBatch) Set(key, value []byte) error {
	cb.pending = append(cb.pending, CacheUpdate{Key: key, Value: value})
	return cb.inner.Set(key, value)
}

func (cb *cachedBatch) Delete(key []byte) error {
	cb.pending = append(cb.pending, CacheUpdate{Key: key, Value: nil})
	return cb.inner.Delete(key)
}

func (cb *cachedBatch) Commit(opts types.WriteOptions) error {
	if err := cb.inner.Commit(opts); err != nil {
		return err
	}
	if err := cb.cache.BatchSet(cb.pending); err != nil {
		return fmt.Errorf("failed to update cache after commit: %w", err)
	}
	cb.pending = nil
	return nil
}

func (cb *cachedBatch) Len() int {
	return cb.inner.Len()
}

func (cb *cachedBatch) Reset() {
	cb.inner.Reset()
	cb.pending = nil
}

func (cb *cachedBatch) Close() error {
	return cb.inner.Close()
}
