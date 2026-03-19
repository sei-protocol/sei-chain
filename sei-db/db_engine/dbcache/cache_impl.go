package dbcache

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"time"

	errorutils "github.com/sei-protocol/sei-chain/sei-db/common/errors"
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

	// The underlying key-value database.
	db types.KeyValueDB

	// The current version number. All modifications to the cache will happen at this version number.
	// This variable is not protected by locks, since it is illegal to update it (i.e. call Snapshot()) concurrently
	// with reads/writes to the most recent version.
	currentVersion uint64

	// The version of the oldest snapshot we are currently tracking.
	oldestVersion uint64

	// Protects modification to versionMap.
	versionLock sync.Mutex

	// Reference counts for all snapshots we are currently tracking. The current (mutable) version
	// does not have a reference count and so it does not appear in this map.
	versionMap map[uint64]*snapshotReferenceCounter
}

// Tracks the reference count for a particular snapshot.
type snapshotReferenceCounter struct {
	// the version of the snapshot
	version uint64

	// the number of references to the snapshot, snapshot is eligible for cleanup when the referenceCount reaches 0
	referenceCount uint64
}

// Creates a new Cache. If cacheName is non-empty, OTel metrics are enabled and the
// background size scrape runs every metricsScrapeInterval.
func NewStandardCache(
	ctx context.Context,
	// The underlying key-value database.
	db types.KeyValueDB,
	// The number of shards in the cache. Must be a power of two and greater than 0.
	shardCount uint64,
	// The maximum size of the cache, in bytes.
	maxSize uint64,
	// A work pool for reading from the DB.
	readPool threading.Pool,
	// A work pool for miscellaneous operations that are neither computationally intensive nor IO bound.
	miscPool threading.Pool,
	// The estimated overhead per entry, in bytes. This is used to calculate the maximum size of the cache.
	// This value should be derived experimentally, and may differ between different builds and architectures.
	estimatedOverheadPerEntry uint64,
	// Name used as the "cache" attribute on metrics. Empty string disables metrics.
	cacheName string,
	// How often to scrape cache size for metrics. Ignored if cacheName is empty.
	metricsScrapeInterval time.Duration,
) (Cache, error) {
	if shardCount == 0 || (shardCount&(shardCount-1)) != 0 {
		return nil, ErrNumShardsNotPowerOfTwo
	}
	if maxSize == 0 {
		return nil, fmt.Errorf("maxSize must be greater than 0")
	}

	shardManager, err := newShardManager(shardCount)
	if err != nil {
		return nil, fmt.Errorf("failed to create shard manager: %w", err)
	}
	sizePerShard := maxSize / shardCount
	if sizePerShard == 0 {
		return nil, fmt.Errorf("maxSize must be greater than shardCount")
	}

	reader := func(key []byte) ([]byte, bool, error) {
		val, err := db.Get(key)
		if err != nil {
			if errors.Is(err, errorutils.ErrNotFound) {
				return nil, false, nil
			}
			return nil, false, fmt.Errorf("failed to read value from database: %w", err)
		}
		return val, true, nil
	}

	shards := make([]*shard, shardCount)
	for i := uint64(0); i < shardCount; i++ {
		shards[i], err = NewShard(ctx, reader, readPool, sizePerShard, estimatedOverheadPerEntry)
		if err != nil {
			return nil, fmt.Errorf("failed to create shard: %w", err)
		}
	}

	c := &cache{
		ctx:            ctx,
		shardManager:   shardManager,
		shards:         shards,
		readPool:       readPool,
		miscPool:       miscPool,
		db:             db,
		versionMap:     make(map[uint64]*snapshotReferenceCounter),
		currentVersion: 0,
		oldestVersion:  0,
		versionLock:    sync.Mutex{},
	}

	if cacheName != "" {
		metrics := newCacheMetrics(ctx, cacheName, metricsScrapeInterval, c.getCacheSizeInfo)
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

func (c *cache) BatchGet(keys map[string]types.BatchGetResult) error {
	return c.BatchGetAtVersion(keys, c.currentVersion)
}

// Similar semantics to BatchGet, but reads from the given version of the cache.
func (c *cache) BatchGetAtVersion(keys map[string]types.BatchGetResult, version uint64) error {
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
			err := c.shards[shardIndex].BatchGet(subMap, version)
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

// Similar semantics to Get, but reads from the given version of the cache.
func (c *cache) Get(key []byte, updateLru bool) ([]byte, bool, error) {
	return c.GetAtVersion(key, c.currentVersion, updateLru)
}

func (c *cache) GetAtVersion(key []byte, version uint64, updateLru bool) ([]byte, bool, error) {
	shardIndex := c.shardManager.Shard(key)
	shard := c.shards[shardIndex]

	value, ok, err := shard.Get(key, version, updateLru)
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

func (c *cache) Snapshot() (CacheSnapshot, error) {
	c.versionLock.Lock()
	defer c.versionLock.Unlock()

	currentVersionRefCounter := &snapshotReferenceCounter{
		version:        c.currentVersion,
		referenceCount: 1,
	}
	c.versionMap[c.currentVersion] = currentVersionRefCounter

	snapshot := &cacheSnapshot{
		version:     c.currentVersion,
		parentCache: c,
	}

	c.currentVersion++

	for _, shard := range c.shards {
		shardVersion := shard.Snapshot()
		if shardVersion != c.currentVersion {
			return nil, fmt.Errorf("shard (%d) has a different version than the cache (%d)",
				shardVersion, c.currentVersion)
		}
	}

	return snapshot, nil
}

// Increment the reference count for the given version.
func (c *cache) IncrementReferenceCount(version uint64) error {
	c.versionLock.Lock()
	defer c.versionLock.Unlock()

	if version < c.oldestVersion {
		return fmt.Errorf("version (%d) is less than the oldest version (%d)", version, c.oldestVersion)
	}
	if version >= c.currentVersion {
		return fmt.Errorf("version (%d) must be less than the current version (%d)", version, c.currentVersion)
	}

	counter, ok := c.versionMap[version]
	if !ok {
		// Should be impossible since the garbage collector won't ever leave gaps
		return fmt.Errorf("version (%d) not found", version)
	}

	if counter.referenceCount == 0 {
		return fmt.Errorf("version (%d) has already been dropped", version)
	}

	counter.referenceCount++
	return nil
}

// Decrement the reference count for the given version.
func (c *cache) DecrementReferenceCount(version uint64) error {
	c.versionLock.Lock()
	defer c.versionLock.Unlock()

	if version < c.oldestVersion {
		return fmt.Errorf("version (%d) is less than the oldest version (%d)", version, c.oldestVersion)
	}
	if version >= c.currentVersion {
		return fmt.Errorf("version (%d) must be less than the current version (%d)", version, c.currentVersion)
	}

	counter, ok := c.versionMap[version]
	if !ok {
		// Should be impossible since the garbage collector won't ever leave gaps
		return fmt.Errorf("version (%d) not found", version)
	}

	if counter.referenceCount == 0 {
		return fmt.Errorf("version (%d) has already been dropped", version)
	}

	counter.referenceCount--
	return nil
}

// Get the diff at a given version.
func (c *cache) GetDiffAtVersion(version uint64) (map[string][]byte, error) {
	diff := make(map[string][]byte)

	for _, shard := range c.shards {
		shardDiff, err := shard.GetDiffsForVersions(version, version)
		if err != nil {
			return nil, fmt.Errorf("failed to get diff from shard: %w", err)
		}

		if len(shardDiff) != 1 {
			return nil, fmt.Errorf("expected 1 diff, got %d", len(shardDiff))
		}

		for key, value := range shardDiff[0] {
			diff[key] = value
		}
	}

	return diff, nil
}
