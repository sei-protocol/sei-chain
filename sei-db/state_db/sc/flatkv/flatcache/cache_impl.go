package flatcache

import (
	"context"
	"fmt"
	"time"

	"github.com/sei-protocol/sei-chain/sei-iavl/proto"
)

var _ Cache = (*cache)(nil)

// A standard implementation of a flatcache.
type cache struct {
	ctx context.Context

	// A utility for assigning keys to shard indices.
	shardManager *shardManager

	// The shards in the cache.
	shards []*shard

	// A scheduler for asyncronous reads.
	readScheduler *readScheduler

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
	// The number of background goroutines to read values from the database.
	readWorkerCount int,
	// The max size of the read queue.
	readQueueSize int,
	// The interval at which to run garbage collection.
	garbageCollectionInterval time.Duration,
) (Cache, error) {
	if shardCount <= 0 || (shardCount&(shardCount-1)) != 0 {
		return nil, ErrNumShardsNotPowerOfTwo
	}
	if maxSize <= 0 {
		return nil, fmt.Errorf("maxSize must be greater than 0")
	}
	if readWorkerCount <= 0 {
		return nil, fmt.Errorf("readWorkerCount must be greater than 0")
	}
	if readQueueSize <= 0 {
		return nil, fmt.Errorf("readQueueSize must be greater than 0")
	}

	shardManager, err := NewShardManager(uint64(shardCount))
	if err != nil {
		return nil, fmt.Errorf("failed to create shard manager: %w", err)
	}

	readScheduler := NewReadScheduler(ctx, readFunc, readWorkerCount, readQueueSize)

	sizePerShard := maxSize / shardCount
	if sizePerShard <= 0 {
		return nil, fmt.Errorf("maxSize must be greater than shardCount")
	}

	shards := make([]*shard, shardCount)
	for i := 0; i < shardCount; i++ {
		shards[i], err = NewShard(readScheduler, sizePerShard)
		if err != nil {
			return nil, fmt.Errorf("failed to create shard: %w", err)
		}
	}

	c := &cache{
		ctx:                       ctx,
		shardManager:              shardManager,
		shards:                    shards,
		readScheduler:             readScheduler,
		garbageCollectionInterval: garbageCollectionInterval,
	}

	go c.runGarbageCollection()

	return c, nil
}

func (c *cache) BatchSet(entries []*proto.KVPair) {

	// First, sort entries by shard index.
	// This allows us to set all values in a single shard with only a single lock acquisition.
	shardMap := make(map[uint64][]*proto.KVPair)
	for _, entry := range entries {
		shardMap[c.shardManager.Shard(entry.Key)] = append(shardMap[c.shardManager.Shard(entry.Key)], entry)
	}

	// This is probably qutie fast, but if it isn't it can be parallelized.
	for shardIndex, shardEntries := range shardMap {
		shard := c.shards[shardIndex]
		shard.BatchSet(shardEntries)
	}
}

func (c *cache) Delete(key []byte) {
	shardIndex := c.shardManager.Shard(key)
	shard := c.shards[shardIndex]
	shard.Delete(key)
}

func (c *cache) Get(key []byte) ([]byte, bool, error) {
	shardIndex := c.shardManager.Shard(key)
	shard := c.shards[shardIndex]

	value, ok, err := shard.Get(key)
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
