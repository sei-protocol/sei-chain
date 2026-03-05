package flatcache

import (
	"context"
	"fmt"

	"github.com/sei-protocol/sei-chain/sei-iavl/proto"
)

var _ Cache = (*cache)(nil)

// A standard implementation of a flatcache.
type cache struct {
	// A utility for assigning keys to shard indices.
	shardManager *shardManager

	// The shards in the cache.
	shards []*shard

	// A scheduler for asyncronous reads.
	readScheduler *readScheduler
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

	return &cache{
		shardManager:  shardManager,
		shards:        shards,
		readScheduler: readScheduler,
	}, nil
}

func (f *cache) BatchSet(entries []*proto.KVPair) {

	// First, sort entries by shard index.
	// This allows us to set all values in a single shard with only a single lock acquisition.
	shardMap := make(map[uint64][]*proto.KVPair)
	for _, entry := range entries {
		shardMap[f.shardManager.Shard(entry.Key)] = append(shardMap[f.shardManager.Shard(entry.Key)], entry)
	}

	// This is probably qutie fast, but if it isn't it can be parallelized.
	for shardIndex, shardEntries := range shardMap {
		shard := f.shards[shardIndex]
		shard.BatchSet(shardEntries)
	}
}

func (f *cache) Delete(key []byte) {
	shardIndex := f.shardManager.Shard(key)
	shard := f.shards[shardIndex]
	shard.Delete(key)
}

func (f *cache) Get(key []byte) ([]byte, bool) {
	shardIndex := f.shardManager.Shard(key)
	shard := f.shards[shardIndex]
	return shard.Get(key)
}

func (f *cache) Set(key []byte, value []byte) {
	shardIndex := f.shardManager.Shard(key)
	shard := f.shards[shardIndex]
	shard.Set(key, value)
}

// TODO create a warming mechanism
