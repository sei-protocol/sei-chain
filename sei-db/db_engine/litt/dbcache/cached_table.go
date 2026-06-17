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
	value, exists, _, err = c.CacheAwareGet(key, false)
	return value, exists, err
}

// In theory, there is a race condition here where call to CacheAwareGet() made concurrently with a call to Put()
// might find the data to exist but not to be hot. This is not a problem though, since it will be hard to trigger and
// since it is not a violation of the consistency/correctness guarantees made by LittDB. Caching is inherently a
// "best effort" optimization, and so it's not worth adding extra locking in order to prevent this edge case.
//
// Scenario:
// - Thread A calls Put() on key K, and Put() does not return right away.
// - Thread B calls CacheAwareGet() on key K with onlyReadFromCache set to true.
// - Thread B checks the cache, and finds that the value is not there.
// - LittDB flushes the value out to disk before thread A's Put() returns, specifically before thread A inserts
//   the value into the write cache. The timing of this is exceptionally unlikely, but not impossible.
// - Thread B gets to the part of CacheAwareGet() where it checks the base table for the value. Since the
//   base table has flushed the value out to disk, it says that the value exists but does not fetch it since
//   onlyReadFromCache is true.
// - Thread A finishes calling Put(), and key K is now in the cache.
//
//   |                     Thread A                                               Thread B
//  Time                      |                                                      |
//   |             Put(key K, ...) starts                                            |
//   v                        |                                                      |
//                            |                                 CacheAwareGet(key K, ...) -> value not present
//                            |                                                      |
//      K is inserted into the unflushed data map                                    |
//                            |                                                      |
//                            |                                 CacheAwareGet(key K, ...) -> present and hot
//                            |                                                      |
//     K is flushed to disk and removed from the unflushed data map                  |
//         (highly irregular but not impossible timing)                              |
//                            |                                                      |
//                            |                                 CacheAwareGet(key K, ...) -> present and cold
//                            |                                                      |
//           K is inserted into the write cache                                      |
//                            |                                                      |
//                            |                                 CacheAwareGet(key K, ...) -> present and hot
//                            |                                                      |
//                  Put (key K, ...) returns                                         |

func (c *cachedTable) CacheAwareGet(
	key []byte,
	onlyReadFromCache bool,
) (value []byte, exists bool, hot bool, err error) {

	if c.metrics != nil {
		start := time.Now()
		defer func() {
			if exists && value != nil {
				c.metrics.ReportReadOperation(c.Name(), time.Since(start), uint64(len(value)), hot)
			}
		}()
	}

	stringKey := util.UnsafeBytesToString(key)

	value, exists = c.writeCache.Get(stringKey)
	if exists {
		// The value was recently written
		hot = true
		return value, exists, hot, err
	} else {
		value, exists = c.readCache.Get(stringKey)
		if exists {
			// The value was recently read
			hot = true
			return value, exists, hot, err
		}
	}

	value, exists, hot, err = c.base.CacheAwareGet(key, onlyReadFromCache)
	if err != nil {
		return value, exists, hot, err
	}

	if exists && value != nil {
		c.readCache.Put(stringKey, value)
	}

	return value, exists, hot, err
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
