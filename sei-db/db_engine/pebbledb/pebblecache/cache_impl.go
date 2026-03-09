package pebblecache

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

// Creates a new Cache.
func NewCache(
	ctx context.Context,
	// A function that reads a value from the database.
	readFunc func(key []byte) []byte,
	// The number of shards in the cache. Must be a power of two and greater than 0.
	shardCount int,
	// The maximum size of the cache, in bytes.
	maxSize int,
	// A work pool for reading from the DB.
	readPool threading.Pool,
	// A work pool for miscellaneous operations that are neither computationally intensive nor IO bound.
	miscPool threading.Pool,
) (Cache, error) {
	if shardCount <= 0 || (shardCount&(shardCount-1)) != 0 {
		return nil, ErrNumShardsNotPowerOfTwo
	}
	if maxSize <= 0 {
		return nil, fmt.Errorf("maxSize must be greater than 0")
	}

	shardManager, err := NewShardManager(uint64(shardCount))
	if err != nil {
		return nil, fmt.Errorf("failed to create shard manager: %w", err)
	}
	sizePerShard := maxSize / shardCount
	if sizePerShard <= 0 {
		return nil, fmt.Errorf("maxSize must be greater than shardCount")
	}

	shards := make([]*shard, shardCount)
	for i := 0; i < shardCount; i++ {
		shards[i], err = NewShard(ctx, readPool, readFunc, sizePerShard)
		if err != nil {
			return nil, fmt.Errorf("failed to create shard: %w", err)
		}
	}

	return &cache{
		ctx:          ctx,
		shardManager: shardManager,
		shards:       shards,
		readPool:     readPool,
		miscPool:     miscPool,
	}, nil
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
			c.shards[shardIndex].BatchSet(shardEntries)
			wg.Done()
		})
		if err != nil {
			return fmt.Errorf("failed to submit batch set: %w", err)
		}
	}
	wg.Wait()

	return nil
}

func (c *cache) BatchGet(keys map[string]types.BatchGetResult) error {
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
			err := c.shards[shardIndex].BatchGet(subMap)
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

func (c *cache) Get(key []byte, updateLru bool) ([]byte, bool, error) {
	shardIndex := c.shardManager.Shard(key)
	shard := c.shards[shardIndex]

	value, ok, err := shard.Get(key, updateLru)
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
	shard.Set(key, value)
}
