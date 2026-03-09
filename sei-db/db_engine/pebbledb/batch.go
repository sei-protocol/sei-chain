package pebbledb

import (
	"fmt"

	"github.com/cockroachdb/pebble/v2"
	"github.com/sei-protocol/sei-chain/sei-db/db_engine/pebbledb/flatcache"
	"github.com/sei-protocol/sei-chain/sei-db/db_engine/types"
)

// pebbleBatch wraps a Pebble batch for atomic writes.
// Important: Callers must call Close() after Commit() to release batch resources,
// even if Commit() succeeds. Failure to Close() will leak memory.
type pebbleBatch struct {
	b     *pebble.Batch
	cache flatcache.Cache

	// Writes are tracked so the cache can be updated after a successful commit.
	pendingCacheUpdates []flatcache.CacheUpdate
}

var _ types.Batch = (*pebbleBatch)(nil)

func newPebbleBatch(db *pebble.DB, cache flatcache.Cache) *pebbleBatch {
	return &pebbleBatch{b: db.NewBatch(), cache: cache}
}

func (p *pebbleDB) NewBatch() types.Batch {
	return newPebbleBatch(p.db, p.cache)
}

func (pb *pebbleBatch) Set(key, value []byte) error {
	pb.pendingCacheUpdates = append(pb.pendingCacheUpdates, flatcache.CacheUpdate{
		Key:   key,
		Value: value,
	})
	return pb.b.Set(key, value, nil)
}

func (pb *pebbleBatch) Delete(key []byte) error {
	pb.pendingCacheUpdates = append(pb.pendingCacheUpdates, flatcache.CacheUpdate{
		Key:      key,
		IsDelete: true,
	})
	return pb.b.Delete(key, nil)
}

func (pb *pebbleBatch) Commit(opts types.WriteOptions) error {
	err := pb.b.Commit(toPebbleWriteOpts(opts))
	if err != nil {
		return fmt.Errorf("failed to commit batch: %w", err)
	}
	err = pb.cache.BatchSet(pb.pendingCacheUpdates)
	if err != nil {
		return fmt.Errorf("failed to set cache: %w", err)
	}
	pb.pendingCacheUpdates = nil
	return nil
}

func (pb *pebbleBatch) Len() int {
	return pb.b.Len()
}

func (pb *pebbleBatch) Reset() {
	pb.b.Reset()
	pb.pendingCacheUpdates = nil
}

func (pb *pebbleBatch) Close() error {
	return pb.b.Close()
}
