package flatcache

import (
	"context"
	"fmt"
	"sync"
	"time"

	"github.com/sei-protocol/sei-chain/sei-db/common/utils"
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

	// A pool for asyncronous reads.
	readPool *utils.WorkPool

	// The interval at which to run garbage collection.
	garbageCollectionInterval time.Duration
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
	readPool *utils.WorkPool,
	// The interval at which to run garbage collection.
	garbageCollectionInterval time.Duration,
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
	if garbageCollectionInterval <= 0 {
		return nil, fmt.Errorf("garbageCollectionInterval must be greater than 0")
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

	c := &cache{
		ctx:                       ctx,
		shardManager:              shardManager,
		shards:                    shards,
		readPool:                  readPool,
		garbageCollectionInterval: garbageCollectionInterval,
	}

	go c.runGarbageCollection()

	return c, nil
}

func (c *cache) BatchSet(updates []CacheUpdate) {
	// Sort entries by shard index so each shard is locked only once.
	shardMap := make(map[uint64][]CacheUpdate)
	for i := range updates {
		idx := c.shardManager.Shard(updates[i].Key)
		shardMap[idx] = append(shardMap[idx], updates[i])
	}

	var wg sync.WaitGroup // TODO use a pool here
	for shardIndex, shardEntries := range shardMap {
		wg.Add(1)
		go func(shardIndex uint64, shardEntries []CacheUpdate) {
			defer wg.Done()
			c.shards[shardIndex].BatchSet(shardEntries)
		}(shardIndex, shardEntries)
	}
	wg.Wait()
}

func (c *cache) BatchGet(keys map[string]types.BatchGetResult) {
	work := make(map[uint64]map[string]types.BatchGetResult)
	for key := range keys {
		idx := c.shardManager.Shard([]byte(key))
		if work[idx] == nil {
			work[idx] = make(map[string]types.BatchGetResult)
		}
		work[idx][key] = types.BatchGetResult{}
	}

	var wg sync.WaitGroup // TODO use a pool here
	for shardIndex, subMap := range work {
		wg.Add(1)
		go func(shardIndex uint64, subMap map[string]types.BatchGetResult) {
			defer wg.Done()
			err := c.shards[shardIndex].BatchGet(subMap)
			if err != nil {
				for key := range subMap {
					subMap[key] = types.BatchGetResult{Error: err}
				}
			}
		}(shardIndex, subMap)
	}
	wg.Wait()

	for _, subMap := range work {
		for key, result := range subMap {
			keys[key] = result
		}
	}
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

// TODO add GC metrics

// Periodically runs garbage collection in the background.
func (c *cache) runGarbageCollection() {

	// Spread out work evenly across all shards, so that we visit each shard roughly once per interval.
	gcSubInterval := c.garbageCollectionInterval / time.Duration(len(c.shards))
	if gcSubInterval == 0 {
		// technically possible if the number of shards is very large and the interval is very small
		gcSubInterval = 1
	}
	ticker := time.NewTicker(gcSubInterval)
	defer ticker.Stop()

	nextShardIndex := 0

	for {
		select {
		case <-c.ctx.Done():
			return
		case <-ticker.C:
			shardIndex := nextShardIndex
			nextShardIndex = (nextShardIndex + 1) % len(c.shards)
			c.shards[shardIndex].RunGarbageCollection()
		}
	}
}

// TODO create a warming mechanism
