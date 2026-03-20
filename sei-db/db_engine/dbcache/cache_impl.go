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
	ctx    context.Context
	cancel context.CancelFunc
	config *CacheConfig

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

	// Protects modification to versionMap, unGCdVersions, lastVersionEligibleForGC, and shuttingDown.
	versionLock *sync.Mutex

	// Reference counts for all snapshots we are currently tracking. The current (mutable) version
	// does not have a reference count and so it does not appear in this map.
	versionMap map[uint64]*snapshotReferenceCounter

	// The number of snapshots that are currently eligible for garbage collection + flush,
	// but which have not yet been flushed.
	unGCdVersions uint64

	// The latest version that is eligible for GC. There may or may not be earlier versions that are eligible for GC.
	// This value is not updated if the version in question is actually GC'd.
	lastVersionEligibleForGC uint64

	// Used to enforce GC backpressure. We want to block Snapshot() if unGCdVersions grows too large.
	gcBackpressueCond *sync.Cond

	// Set to true when the cache is shutting down.
	shuttingDown bool

	// Closed by the GC runner when it exits, so Close() can wait for it.
	gcDone chan struct{}

	// Whether the first Snapshot() has already attempted to load the boot hash from the DB.
	bootHashLoaded bool

	// True only if the boot hash was successfully found in the DB; controls whether
	// GC gates on hash being set.
	hashTrackingEnabled bool

	// The hash loaded from the DB at boot time. Used once to populate the first snapshot,
	// then cleared.
	bootHash []byte
}

// Tracks the reference count for a particular snapshot.
type snapshotReferenceCounter struct {
	// the version of the snapshot
	version uint64

	// the number of references to the snapshot, snapshot is eligible for cleanup when the referenceCount reaches 0
	referenceCount uint64

	// The hash associated with this snapshot (nil = not yet set).
	// Set via SetHash after the snapshot is created (or auto-loaded from DB for the boot snapshot).
	hash []byte

	// Closed by SetHash to wake AwaitHash waiters. Only allocated when hash tracking is enabled.
	hashReady chan struct{}
}

// Creates a new Cache.
func NewStandardCache(
	ctx context.Context,
	config *CacheConfig,
	// The underlying key-value database.
	db types.KeyValueDB,
	// A work pool for reading from the DB.
	readPool threading.Pool,
	// A work pool for miscellaneous operations that are neither computationally intensive nor IO bound.
	miscPool threading.Pool,
) (Cache, error) {
	if err := config.Validate(); err != nil {
		return nil, fmt.Errorf("invalid cache config: %w", err)
	}

	shardManager, err := newShardManager(config.ShardCount)
	if err != nil {
		return nil, fmt.Errorf("failed to create shard manager: %w", err)
	}
	sizePerShard := config.MaxSize / config.ShardCount

	reader := ReaderFromDB(db)

	shards := make([]*shard, config.ShardCount)
	for i := uint64(0); i < config.ShardCount; i++ {
		shards[i], err = NewShard(ctx, config, reader, readPool, sizePerShard)
		if err != nil {
			return nil, fmt.Errorf("failed to create shard: %w", err)
		}
	}

	versionLock := &sync.Mutex{}
	gcBackpressueCond := sync.NewCond(versionLock)

	childCtx, cancel := context.WithCancel(ctx)

	c := &cache{
		ctx:               childCtx,
		cancel:            cancel,
		config:            config,
		shardManager:      shardManager,
		shards:            shards,
		readPool:          readPool,
		miscPool:          miscPool,
		db:                db,
		versionMap:        make(map[uint64]*snapshotReferenceCounter),
		currentVersion:    1, // important: versions start at 1, not 0, to allow version-1 without underflow
		oldestVersion:     1,
		versionLock:       versionLock,
		gcBackpressueCond: gcBackpressueCond,
		gcDone:            make(chan struct{}),
	}

	if config.MetricsEnabled {
		metrics := newCacheMetrics(ctx, config.MetricsName, config.MetricsScrapeInterval(), c.getCacheSizeInfo)
		for _, s := range c.shards {
			s.metrics = metrics
		}
	}

	go c.garbageCollectionRunner()

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
	if !c.bootHashLoaded {
		if err := c.initBootHash(); err != nil {
			return nil, err
		}
	}

	c.versionLock.Lock()
	defer c.versionLock.Unlock()

	err := c.gcBackpressure()
	if err != nil {
		return nil, fmt.Errorf("failed wait for gc to catch up: %w", err)
	}

	var hashReady chan struct{}
	if c.hashTrackingEnabled {
		hashReady = make(chan struct{})
	}

	currentVersionRefCounter := &snapshotReferenceCounter{
		version:        c.currentVersion,
		referenceCount: 1,
		hashReady:      hashReady,
	}

	if c.bootHash != nil {
		currentVersionRefCounter.hash = c.bootHash
		if hashReady != nil {
			close(hashReady)
		}
		c.bootHash = nil
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

// initBootHash loads the hash from the underlying DB on the first Snapshot() call.
// If HashKey is not configured, hash tracking is disabled. If the hash key is
// configured but missing from a non-empty DB, an error is returned.
func (c *cache) initBootHash() error {
	c.bootHashLoaded = true

	if len(c.config.HashKey) == 0 {
		return nil
	}

	val, err := c.db.Get(c.config.HashKey)
	if err != nil {
		if !errors.Is(err, errorutils.ErrNotFound) {
			return fmt.Errorf("failed to read boot hash from DB: %w", err)
		}

		empty, emptyErr := c.isDBEmpty()
		if emptyErr != nil {
			return fmt.Errorf("failed to check if DB is empty: %w", emptyErr)
		}
		if !empty {
			return fmt.Errorf("hash key %q missing from non-empty DB", string(c.config.HashKey))
		}
		return nil
	}

	c.hashTrackingEnabled = true
	c.bootHash = val
	return nil
}

func (c *cache) isDBEmpty() (bool, error) {
	iter, err := c.db.NewIter(nil)
	if err != nil {
		return false, err
	}
	hasData := iter.First()
	if err := iter.Close(); err != nil {
		return false, err
	}
	return !hasData, nil
}

// This method blocks if GC/flush is not keeping up. It is assumed that the caller already holds the versionLock.
// When this method returns, it will still hold the versionLock, but it may release and then re-acquire versionLock
// internally as it awaits for GC to catch up.
func (c *cache) gcBackpressure() error {
	for c.unGCdVersions > c.config.MaxUnGCdVersions {
		if c.shuttingDown {
			return fmt.Errorf("context cancelled")
		}
		c.gcBackpressueCond.Wait()
	}
	return nil
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

	c.scanGCEligibility()

	return nil
}

// scanGCEligibility iterates from lastVersionEligibleForGC+1 forward and marks
// contiguous eligible versions. A version is eligible when its reference count
// is 0 and (if hash tracking is enabled) its hash has been set.
// Caller must hold versionLock.
func (c *cache) scanGCEligibility() {
	versionToConsider := c.lastVersionEligibleForGC + 1
	for {
		counter, ok := c.versionMap[versionToConsider]
		if !ok {
			break
		}

		if !c.isVersionGCEligible(counter) {
			break
		}

		c.unGCdVersions++
		c.lastVersionEligibleForGC = versionToConsider
		versionToConsider++
	}
}

func (c *cache) isVersionGCEligible(counter *snapshotReferenceCounter) bool {
	if counter.referenceCount > 0 {
		return false
	}
	if c.hashTrackingEnabled && counter.hash == nil {
		return false
	}
	return true
}

// SetSnapshotHash attaches a hash to the snapshot at the given version.
func (c *cache) SetSnapshotHash(version uint64, hash []byte) error {
	if !c.hashTrackingEnabled {
		return fmt.Errorf("snapshot hashing is disabled")
	}
	if hash == nil {
		return fmt.Errorf("hash must not be nil")
	}

	c.versionLock.Lock()
	defer c.versionLock.Unlock()

	counter, ok := c.versionMap[version]
	if !ok {
		return fmt.Errorf("version (%d) not found", version)
	}

	if counter.hash != nil {
		return fmt.Errorf("hash already set for version %d", version)
	}

	counter.hash = hash
	if counter.hashReady != nil {
		close(counter.hashReady)
	}

	c.scanGCEligibility()

	return nil
}

// GetSnapshotHash returns the hash for the snapshot at the given version.
func (c *cache) GetSnapshotHash(version uint64) ([]byte, error) {
	c.versionLock.Lock()
	defer c.versionLock.Unlock()

	counter, ok := c.versionMap[version]
	if !ok {
		return nil, fmt.Errorf("version (%d) not found", version)
	}

	if counter.hash == nil {
		return nil, fmt.Errorf("hash not yet set for version %d", version)
	}

	return counter.hash, nil
}

// AwaitSnapshotHash blocks until the hash for the given version is available.
func (c *cache) AwaitSnapshotHash(ctx context.Context, version uint64) ([]byte, error) {
	if !c.hashTrackingEnabled {
		return nil, fmt.Errorf("snapshot hashing is disabled")
	}

	c.versionLock.Lock()
	counter, ok := c.versionMap[version]
	if !ok {
		c.versionLock.Unlock()
		return nil, fmt.Errorf("version (%d) not found", version)
	}

	if counter.hash != nil {
		hash := counter.hash
		c.versionLock.Unlock()
		return hash, nil
	}

	hashReady := counter.hashReady
	c.versionLock.Unlock()

	_, err := threading.InterruptiblePull(ctx, hashReady)
	if err != nil {
		return nil, fmt.Errorf("failed to await hash: %w", err)
	}

	return counter.hash, nil
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

// Perform periodic garbage collection.
func (c *cache) garbageCollectionRunner() {
	defer close(c.gcDone)

	ticker := time.NewTicker(c.config.GCInterval())
	defer ticker.Stop()

	for {
		select {
		case <-c.ctx.Done():
			// Send a signal to wake up Snapshot() if it is blocked by backpressure.
			c.versionLock.Lock()
			c.shuttingDown = true
			c.versionLock.Unlock()
			c.gcBackpressueCond.Signal()
			return
		case <-ticker.C:
			err := c.garbageCollect()
			if err != nil {
				// It's unfortunate to have to panic here, but GC failure is not recoverable.
				// Continuing in the face of GC failure is likely to lead to GC corruption
				// and a divergant app hash.
				panic(fmt.Sprintf("failed to garbage collect: %v", err))
			}
		}
	}
}

// Garbage collect all eligible snapshots and push data down into the DB.
func (c *cache) garbageCollect() error {

	// Determine which versions need cleanup.
	firstVersionToGC := c.oldestVersion
	lastVersionToGC := c.oldestVersion - 1
	c.versionLock.Lock()

	oldestVersion := c.oldestVersion
	for version := oldestVersion; version < c.currentVersion; version++ {
		counter, ok := c.versionMap[version]
		if !ok {
			// should be impossible
			c.versionLock.Unlock()
			return fmt.Errorf("version (%d) not found in version map", version)
		}
		if !c.isVersionGCEligible(counter) {
			break
		}
		lastVersionToGC++
		c.oldestVersion++
		delete(c.versionMap, version)
	}

	c.versionLock.Unlock()

	if lastVersionToGC < firstVersionToGC {
		return nil
	}

	return c.flushVersions(firstVersionToGC, lastVersionToGC)
}

// flushVersions collects diffs for [firstVersion, lastVersion] from all shards,
// writes them to the underlying DB in batches, then drops the versions from the shards.
func (c *cache) flushVersions(firstVersion, lastVersion uint64) error {

	// Collect diffs from all shards.
	diffsByVersion := make(map[uint64]map[string][]byte)
	for version := firstVersion; version <= lastVersion; version++ {
		diffsByVersion[version] = make(map[string][]byte)
	}
	for _, shard := range c.shards {
		shardDiffs, err := shard.GetDiffsForVersions(firstVersion, lastVersion)
		if err != nil {
			return fmt.Errorf("failed to get diffs for shard: %w", err)
		}
		for diffIndex, diff := range shardDiffs {
			version := firstVersion + uint64(diffIndex) //nolint:gosec // diffIndex is bounded by version count
			for key, value := range diff {
				diffsByVersion[version][key] = value
			}
		}
	}

	// Write diffs to the DB in batches, oldest version first.
	var batch types.Batch
	versionsInBatch := uint64(0)
	for version := firstVersion; version <= lastVersion; version++ {
		versionsInBatch++
		if batch == nil {
			batch = c.db.NewBatch()
		}
		for key, value := range diffsByVersion[version] {
			if value == nil {
				if err := batch.Delete([]byte(key)); err != nil {
					return fmt.Errorf("flush failed to delete key: %w", err)
				}
			} else {
				if err := batch.Set([]byte(key), value); err != nil {
					return fmt.Errorf("flush failed to set key: %w", err)
				}
			}
		}

		if batch.Len() >= c.config.TargetKeysPerFlush {
			if err := batch.Commit(types.WriteOptions{Sync: true}); err != nil {
				return fmt.Errorf("flush failed to commit batch: %w", err)
			}
			batch = nil
			c.versionLock.Lock()
			c.unGCdVersions -= versionsInBatch
			c.versionLock.Unlock()
			c.gcBackpressueCond.Signal()
			versionsInBatch = 0
		}
	}
	if batch != nil {
		if err := batch.Commit(types.WriteOptions{Sync: true}); err != nil {
			return fmt.Errorf("flush failed to commit batch: %w", err)
		}
		c.versionLock.Lock()
		c.unGCdVersions -= versionsInBatch
		c.versionLock.Unlock()
		c.gcBackpressueCond.Signal()
	}

	// Now that the data is reflected in the DB, clean up the shards.
	for _, shard := range c.shards {
		if err := shard.DropVersions(firstVersion, lastVersion); err != nil {
			panic(fmt.Sprintf("failed to drop versions from shard: %v", err))
		}
	}

	return nil
}

// UnderlyingDB returns the raw backing database. Intended for test-only use
// (e.g. iteration for ground-truth verification). Production code should use
// the Cache interface methods.
func (c *cache) UnderlyingDB() types.KeyValueDB {
	return c.db
}

func (c *cache) Close() error {
	// Stop the GC runner and wait for it to exit.
	c.cancel()
	<-c.gcDone

	// Flush all remaining snapshotted versions to disk. The current mutable version
	// is intentionally not flushed — if the caller wants it persisted, they should
	// finalize it via Snapshot() before calling Close().
	if c.oldestVersion < c.currentVersion {
		if err := c.flushVersions(c.oldestVersion, c.currentVersion-1); err != nil {
			return fmt.Errorf("close: failed to flush remaining versions: %w", err)
		}
	}

	return c.db.Close()
}
