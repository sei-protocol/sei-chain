package dbcache

import (
	"fmt"
	"time"

	"github.com/sei-protocol/sei-chain/sei-db/db_engine/litt"
	"github.com/sei-protocol/sei-chain/sei-db/db_engine/litt/metrics"
	"github.com/sei-protocol/sei-chain/sei-db/db_engine/litt/types"
	"github.com/sei-protocol/sei-chain/sei-db/db_engine/litt/util"
)

var _ litt.ManagedTable = &cachedTable{}

// cachedTable wraps a table and adds caching functionality.
type cachedTable struct {
	// The base table to wrap.
	base litt.ManagedTable
	// This cache holds values that were recently written to the table.
	writeCache util.Cache[string, []byte]
	// This cache holds values that were recently read from the base table.
	readCache util.Cache[string, []byte]
	// Metrics for the table.
	metrics *metrics.LittDBMetrics
}

// NewCachedTable creates wrapper around a table that caches recently written and read values.
func NewCachedTable(
	base litt.ManagedTable,
	writeCache util.Cache[string, []byte],
	readCache util.Cache[string, []byte],
	metrics *metrics.LittDBMetrics,
) litt.ManagedTable {
	return &cachedTable{
		base:       base,
		writeCache: writeCache,
		readCache:  readCache,
		metrics:    metrics,
	}
}

func (c *cachedTable) KeyCount() uint64 {
	return c.base.KeyCount()
}

func (c *cachedTable) Size() uint64 {
	return c.base.Size()
}

func (c *cachedTable) Name() string {
	return c.base.Name()
}

func (c *cachedTable) Put(key []byte, value []byte, secondaryKeys ...*types.SecondaryKey) error {
	err := c.base.Put(key, value, secondaryKeys...)
	if err != nil {
		return fmt.Errorf("failed to put entry into base table: %w", err)
	}
	c.writeCache.Put(string(key), value)
	for _, sk := range secondaryKeys {
		c.writeCache.Put(string(sk.Key), value[sk.Offset:sk.Offset+sk.Length])
	}
	return nil
}

func (c *cachedTable) PutBatch(batch []*types.PutRequest) error {
	err := c.base.PutBatch(batch)
	if err != nil {
		return err
	}
	for _, req := range batch {
		c.writeCache.Put(util.UnsafeBytesToString(req.Key), req.Value)
		for _, sk := range req.SecondaryKeys {
			c.writeCache.Put(util.UnsafeBytesToString(sk.Key), req.Value[sk.Offset:sk.Offset+sk.Length])
		}
	}
	return nil
}

func (c *cachedTable) Get(key []byte) (value []byte, exists bool, err error) {
	// hot tracks whether the value was served from one of this table's caches (a "hot" read) for metrics.
	var hot bool
	if c.metrics != nil {
		start := time.Now()
		defer func() {
			if exists && value != nil {
				c.metrics.ReportReadOperation(c.Name(), time.Since(start), uint64(len(value)), hot)
			}
		}()
	}

	stringKey := util.UnsafeBytesToString(key)

	if value, exists = c.writeCache.Get(stringKey); exists {
		// The value was recently written.
		hot = true
		return value, exists, nil
	}
	if value, exists = c.readCache.Get(stringKey); exists {
		// The value was recently read.
		hot = true
		return value, exists, nil
	}

	value, exists, err = c.base.Get(key)
	if err != nil {
		return value, exists, fmt.Errorf("failed to get entry from base table: %w", err)
	}
	if exists && value != nil {
		c.readCache.Put(stringKey, value)
	}

	return value, exists, nil
}

// GetSubrange reads only the [offset, offset+length) byte range of the value stored under key.
//
// Cache policy — subrange reads consult both caches, but never populate them:
//
//   - Populating would be unsafe. Both caches are keyed by the full key and hold the full value, so
//     storing a sub-range under that key would hand a later Get a partial value it is entitled to treat
//     as the whole thing.
//   - Consulting is safe, because every path agrees that a key's offsets are relative to that key's own
//     logical value. Put stores value[sk.Offset:sk.Offset+sk.Length] under a secondary key, the base table
//     addresses that secondary at the same aliased region on disk, and readCache holds whatever Get
//     returned. A cache hit therefore slices exactly the bytes the base table would have read.
//   - Consulting is also strictly cheaper. A hit means the value is already materialized in memory, so
//     slicing it costs no I/O at all, where the base table would pay a keymap lookup plus a disk read.
//     Bypassing the caches would make GetSubrange slower than plain Get for precisely the hot,
//     recently-written values a subrange read is meant to serve.
//
// Because hits come from the same caches Get uses, the two agree on whether a key exists — including for
// a key already reclaimed from the base table by GC or TTL expiry but still resident in a cache.
//
// Caching sub-ranges themselves, under specially structured keys, would speed up misses as well, but that
// adds complexity we do not yet know we need. Revisit if profiling shows subrange misses are hot enough to
// justify it.
func (c *cachedTable) GetSubrange(key []byte, offset uint32, length uint32) (value []byte, exists bool, err error) {
	// hot tracks whether the value was served from one of this table's caches (a "hot" read) for metrics.
	var hot bool
	if c.metrics != nil {
		start := time.Now()
		defer func() {
			if exists && value != nil {
				c.metrics.ReportReadOperation(c.Name(), time.Since(start), uint64(len(value)), hot)
			}
		}()
	}

	stringKey := util.UnsafeBytesToString(key)

	if cached, ok := c.writeCache.Get(stringKey); ok {
		// The value was recently written.
		hot = true
		return subrangeOf(cached, offset, length)
	}
	if cached, ok := c.readCache.Get(stringKey); ok {
		// The value was recently read in full.
		hot = true
		return subrangeOf(cached, offset, length)
	}

	value, exists, err = c.base.GetSubrange(key, offset, length)
	if err != nil {
		return value, exists, fmt.Errorf("failed to get subrange from base table: %w", err)
	}
	return value, exists, nil
}

// subrangeOf slices a cached full value, applying the same bounds check (and reporting it the same way)
// as the base table, so a cache hit and a cache miss are indistinguishable to the caller.
func subrangeOf(value []byte, offset uint32, length uint32) ([]byte, bool, error) {
	end := uint64(offset) + uint64(length)
	if end > uint64(len(value)) {
		return nil, false, fmt.Errorf(
			"subrange [%d, %d) is out of bounds for value of length %d", offset, end, len(value))
	}
	// Capped (three-index) slice: without it the sub-range would carry spare capacity reaching into the
	// rest of the cached value, and an append by the caller would silently overwrite the bytes that
	// follow — corrupting the entry for every later reader. Capping forces such an append to allocate.
	return value[offset:end:end], true, nil
}

func (c *cachedTable) Exists(key []byte) (exists bool, err error) {
	_, exists = c.writeCache.Get(util.UnsafeBytesToString(key))
	if exists {
		return true, nil
	}

	_, exists = c.readCache.Get(util.UnsafeBytesToString(key))
	if exists {
		return true, nil
	}

	return c.base.Exists(key)
}

func (c *cachedTable) Flush() error {
	return c.base.Flush()
}

func (c *cachedTable) SetTTL(ttl time.Duration) error {
	return c.base.SetTTL(ttl)
}

func (c *cachedTable) SetWriteCacheSize(size uint64) error {
	c.writeCache.SetMaxWeight(size)
	err := c.base.SetWriteCacheSize(size)
	if err != nil {
		return fmt.Errorf("failed to set base table write cache size: %w", err)
	}
	return nil
}

func (c *cachedTable) SetReadCacheSize(size uint64) error {
	c.readCache.SetMaxWeight(size)
	err := c.base.SetReadCacheSize(size)
	if err != nil {
		return fmt.Errorf("failed to set base table read cache size: %w", err)
	}
	return nil
}

func (c *cachedTable) Close() error {
	return c.base.Close()
}

func (c *cachedTable) Drop() error {
	return c.base.Drop()
}

func (c *cachedTable) IsDropped() bool {
	return c.base.IsDropped()
}

func (c *cachedTable) SetShardingFactor(shardingFactor uint8) error {
	return c.base.SetShardingFactor(shardingFactor)
}

func (c *cachedTable) RunGC() error {
	return c.base.RunGC()
}

// Iterator returns a new iterator over the keys in the table. The iterator reads values directly from
// the base table, bypassing the cache: the iterator's target workload is a large linear scan, for which
// the cache offers no benefit and would only thrash.
func (c *cachedTable) Iterator(reverse bool) (litt.Iterator, error) {
	return c.base.Iterator(reverse)
}

// IteratorAt returns a new iterator positioned at key. Like Iterator, it reads values directly from the
// base table, bypassing the cache.
func (c *cachedTable) IteratorAt(key []byte, reverse bool) (litt.Iterator, bool, error) {
	return c.base.IteratorAt(key, reverse)
}

func (c *cachedTable) GetOldestKey() (key []byte, exists bool, err error) {
	return c.base.GetOldestKey()
}

func (c *cachedTable) GetNewestKey() (key []byte, exists bool, err error) {
	return c.base.GetNewestKey()
}
