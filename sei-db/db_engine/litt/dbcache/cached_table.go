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

func (c *cachedTable) GetOldestKey() (key []byte, exists bool, err error) {
	return c.base.GetOldestKey()
}

func (c *cachedTable) GetNewestKey() (key []byte, exists bool, err error) {
	return c.base.GetNewestKey()
}
