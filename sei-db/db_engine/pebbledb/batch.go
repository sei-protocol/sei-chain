package pebbledb

import (
	"github.com/cockroachdb/pebble/v2"
	"github.com/sei-protocol/sei-chain/sei-db/db_engine/pebbledb/flatcache"
	"github.com/sei-protocol/sei-chain/sei-db/db_engine/types"
)

type pendingCacheWrite struct {
	key      []byte
	value    []byte
	isDelete bool
}

// pebbleBatch wraps a Pebble batch for atomic writes.
// Important: Callers must call Close() after Commit() to release batch resources,
// even if Commit() succeeds. Failure to Close() will leak memory.
type pebbleBatch struct {
	b     *pebble.Batch
	cache flatcache.Cache

	// Writes are tracked so the cache can be updated after a successful commit.
	pendingCacheWrites []pendingCacheWrite
}

var _ types.Batch = (*pebbleBatch)(nil)

func newPebbleBatch(db *pebble.DB, cache flatcache.Cache) *pebbleBatch {
	return &pebbleBatch{b: db.NewBatch(), cache: cache}
}

func (p *pebbleDB) NewBatch() types.Batch {
	return newPebbleBatch(p.db, p.cache)
}

func (pb *pebbleBatch) Set(key, value []byte) error {
	pb.pendingCacheWrites = append(pb.pendingCacheWrites, pendingCacheWrite{
		key:   key,
		value: value,
	})
	return pb.b.Set(key, value, nil)
}

func (pb *pebbleBatch) Delete(key []byte) error {
	pb.pendingCacheWrites = append(pb.pendingCacheWrites, pendingCacheWrite{
		key:      key,
		isDelete: true,
	})
	return pb.b.Delete(key, nil)
}

func (pb *pebbleBatch) Commit(opts types.WriteOptions) error {
	err := pb.b.Commit(toPebbleWriteOpts(opts))
	if err != nil {
		return err
	}
	for _, w := range pb.pendingCacheWrites {
		if w.isDelete {
			pb.cache.Delete(w.key)
		} else {
			pb.cache.Set(w.key, w.value)
		}
	}
	pb.pendingCacheWrites = nil
	return nil
}

func (pb *pebbleBatch) Len() int {
	return pb.b.Len()
}

func (pb *pebbleBatch) Reset() {
	pb.b.Reset()
	pb.pendingCacheWrites = nil
}

func (pb *pebbleBatch) Close() error {
	return pb.b.Close()
}
