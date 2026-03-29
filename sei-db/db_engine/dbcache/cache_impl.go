package dbcache

import (
	"context"
	"fmt"
	"sync"

	"github.com/sei-protocol/sei-chain/sei-db/common/threading"
	"github.com/sei-protocol/sei-chain/sei-db/db_engine/types"
)

var _ Cache = (*cache)(nil)

// A standard implementation of a flatcache.
type cache struct {
	ctx context.Context

	// A utility for assigning keys to shard indices.
	shardManager *shardManager

	// The shards in the cache.
	shards []*shard

	// A pool for asynchronous reads.
	readPool threading.Pool

	// A pool for miscellaneous operations that are neither computationally intensive nor IO bound.
	miscPool threading.Pool
}

// Creates a new Cache. If cfg.MetricsName is non-empty, OTel metrics are enabled and the
// background size scrape runs every cfg.MetricsScrapeInterval.
func NewStandardCache(
	ctx context.Context,
	cfg *CacheConfig,
	readPool threading.Pool,
	miscPool threading.Pool,
) (Cache, error) {
	if cfg.ShardCount == 0 || (cfg.ShardCount&(cfg.ShardCount-1)) != 0 {
		return nil, ErrNumShardsNotPowerOfTwo
	}
	if cfg.MaxSize == 0 {
		return nil, fmt.Errorf("maxSize must be greater than 0")
	}

	shardManager, err := newShardManager(cfg.ShardCount)
	if err != nil {
		return nil, fmt.Errorf("failed to create shard manager: %w", err)
	}
	sizePerShard := cfg.MaxSize / cfg.ShardCount
	if sizePerShard == 0 {
		return nil, fmt.Errorf("maxSize must be greater than shardCount")
	}

	shards := make([]*shard, cfg.ShardCount)
	for i := uint64(0); i < cfg.ShardCount; i++ {
		shards[i], err = NewShard(ctx, readPool, sizePerShard, cfg.EstimatedOverheadPerEntry)
		if err != nil {
			return nil, fmt.Errorf("failed to create shard: %w", err)
		}
	}

	c := &cache{
		ctx:          ctx,
		shardManager: shardManager,
		shards:       shards,
		readPool:     readPool,
		miscPool:     miscPool,
	}

	if cfg.MetricsName != "" {
		metrics := newCacheMetrics(ctx, cfg.MetricsName, cfg.MetricsScrapeInterval, c.getCacheSizeInfo)
		for _, s := range c.shards {
			s.metrics = metrics
		}
	}

	return c, nil
}

func (c *cache) getCacheSizeInfo() (bytes uint64, entries uint64) {
	for _, s := range c.shards {
		b, e := s.getSizeInfo()
		bytes += b
		entries += e
	}
	return bytes, entries
}

func (c *cache) BatchSet(updates []CacheUpdate) error {
	// Sort entries by shard index so each shard is locked only once.
	shardMap := make(map[uint64][]CacheUpdate)
	for i := range updates {
		idx := c.shardManager.Shard(updates[i].Key)
		shardMap[idx] = append(shardMap[idx], updates[i])
	}

	var wg sync.WaitGroup
	for shardIndex, shardEntries := range shardMap {
		wg.Add(1)
		err := c.miscPool.Submit(c.ctx, func() {
			defer wg.Done()
			c.shards[shardIndex].BatchSet(shardEntries)
		})
		if err != nil {
			return fmt.Errorf("failed to submit batch set: %w", err)
		}
	}
	wg.Wait()

	return nil
}

func (c *cache) BatchGet(read Reader, keys map[string]types.BatchGetResult) error {
	work := make(map[uint64]map[string]types.BatchGetResult)
	for key := range keys {
		idx := c.shardManager.Shard([]byte(key))
		if work[idx] == nil {
			work[idx] = make(map[string]types.BatchGetResult)
		}
		work[idx][key] = types.BatchGetResult{}
	}

	var wg sync.WaitGroup
	for shardIndex, subMap := range work {
		wg.Add(1)

		err := c.miscPool.Submit(c.ctx, func() {
			defer wg.Done()
			err := c.shards[shardIndex].BatchGet(read, subMap)
			if err != nil {
				for key := range subMap {
					subMap[key] = types.BatchGetResult{Error: err}
				}
			}
		})
		if err != nil {
			return fmt.Errorf("failed to submit batch get: %w", err)
		}
	}
	wg.Wait()

	for _, subMap := range work {
		for key, result := range subMap {
			keys[key] = result
		}
	}

	return nil
}

func (c *cache) Delete(key []byte) {
	shardIndex := c.shardManager.Shard(key)
	shard := c.shards[shardIndex]
	shard.Delete(key)
}

func (c *cache) Get(read Reader, key []byte, updateLru bool) ([]byte, bool, error) {
	shardIndex := c.shardManager.Shard(key)
	shard := c.shards[shardIndex]

	value, ok, err := shard.Get(read, key, updateLru)
	if err != nil {
		return nil, false, fmt.Errorf("failed to get value from shard: %w", err)
	}
	if !ok {
		return nil, false, nil
	}
	return value, ok, nil
}

func (c *cache) Set(key []byte, value []byte) {
	shardIndex := c.shardManager.Shard(key)
	shard := c.shards[shardIndex]

	if value == nil {
		shard.Delete(key)
	} else {
		shard.Set(key, value)
	}
}
