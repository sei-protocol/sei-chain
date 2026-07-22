package snapshot

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"sort"
	"sync"

	"github.com/sei-protocol/sei-chain/sei-db/common/threading"
	"github.com/sei-protocol/sei-chain/sei-db/db_engine/types"
)

var _ SnapshotEngine = (*snapshotEngine)(nil)

// The standard implementation of SnapshotEngine.
type snapshotEngine struct {
	ctx    context.Context
	cancel context.CancelFunc
	config *SnapshotEngineConfig

	// A utility for assigning keys to shard indices.
	shardManager *shardManager

	// The shards in the engine.
	shards []*shard

	// A pool for asynchronous reads.
	readPool threading.Pool

	// A pool for miscellaneous operations that are neither computationally intensive nor IO bound.
	miscPool threading.Pool

	// The underlying key-value database.
	db types.KeyValueDB

	// Protects modification to version state.
	versionLock *sync.Mutex

	// The current version number. All modifications to the engine will happen at this version number.
	// This variable is not protected by locks, since it is illegal to update it (i.e. call Snapshot()) concurrently
	// with reads/writes to the most recent version.
	//
	// Protected by versionLock.
	currentVersion uint64

	// The version of the oldest snapshot we are currently tracking.
	//
	// Protected by versionLock.
	oldestVersion uint64

	// Reference counts for all snapshots we are currently tracking. The current (mutable) version
	// does not have a reference count and so it does not appear in this map.
	//
	// Protected by versionLock.
	versionMap map[uint64]*snapshotReferenceCounter

	// The number of snapshots that are eligible to be flushed to disk, but which have not yet been flushed.
	//
	// Protected by versionLock.
	unflushedCount uint64

	// The highest snapshot version that either has been flushed or can be flushed. There may be older snapshot
	// versions that have not yet been flushed, but it is guaranteed that any snapshot version older than this value
	// is likewise eligible to be flushed or has already been flushed.
	//
	// Protected by versionLock.
	highestFlushEligibleVersion uint64

	// The highest snapshot version that is eligible for retirement.
	//
	// Protected by versionLock.
	highestRetirementEligibleVersion uint64

	// Used to enforce lifecycle backpressure. We want to block Snapshot() if unretiredVersions grows too large.
	lifecycleBackpressureCond *sync.Cond

	// Signaled (non-blocking) by scanRetirementEligibility when new versions become
	// eligible for retirement. The lifecycle runner selects on this to wake up.
	lifecycleWake chan struct{}

	// Iterator construction is dispatched here so it runs serialized with flush
	// and retire on the lifecycle goroutine — see iteratorRequest for the full
	// rationale. Unbuffered so callers naturally backpressure when the
	// lifecycle goroutine is busy.
	iteratorRequests chan iteratorRequest

	// A struct{} is sent on this channel when the lifecycle runner should exit. The lifecycle goroutine
	// signals that it has exited by closing lifecycleExited (below), which Close waits on.
	lifecycleExit chan struct{}

	// Closed by the lifecycle goroutine immediately before it returns. Close waits on this to ensure
	// the lifecycle runner has fully stopped before performing its final flush.
	lifecycleExited chan struct{}

	// Metrics for recording snapshot engine statistics.
	metrics *SnapshotEngineMetrics
}

// Tracks the reference count for a particular snapshot.
type snapshotReferenceCounter struct {
	// The version/block height of the snapshot.
	version uint64

	// The number of reservations currently held for this snapshot. When this count reaches 0,
	// the snapshot is eligible for retirement.
	referenceCount uint64

	// Opaque hash bytes for this snapshot. Nil until SetSnapshotHash is called. By contract, the
	// hashing subsystem holds a reference count on the snapshot until it has set the hash, so this
	// is guaranteed to be non-nil before referenceCount reaches 0. Enforced in DecrementReferenceCount.
	hash []byte

	// Closed by SetHash to wake AwaitHash waiters.
	hashReady chan struct{}

	// True if the snapshot has been flushed, otherwise false.
	flushedToDisk bool

	// Closed by flushSnapshots when this snapshot's data has been persisted to disk. Pairs with
	// flushedToDisk. Used as a synchronization handle for AwaitFlush waiters; a closed channel is
	// immediately selectable, so the "already flushed at call time" case requires no special path.
	flushCompleted chan struct{}
}

// iteratorRequest is sent by snapshotImpl.Iterator() to the lifecycle
// goroutine, which builds the iterator and replies on response.
//
// Iterator construction is routed through the lifecycle goroutine so that
// materializing in-memory overrides and opening the DB iterator happen
// serialized with flush and retire. Without serialization, data that moves
// from versionedData into the DB between the two halves could be lost. The
// lifecycle goroutine is the only thing that performs flush/retire, so doing
// the work there guarantees consistency without extra locks.
//
// Iteration cost is already dominated by the DB iterator itself, so it is
// acceptable for an Iterator() call to wait behind an in-progress flush.
type iteratorRequest struct {
	version  uint64
	response chan iteratorResponse
}

// iteratorResponse carries the result of snapshotEngine.buildIterator back to the
// caller. Exactly one of iter / err is non-nil.
type iteratorResponse struct {
	iter Iterator
	err  error
}

// Creates a new SnapshotEngine.
func NewSnapshotEngine(
	ctx context.Context,
	config *SnapshotEngineConfig,
	// The underlying key-value database.
	db types.KeyValueDB,
	// A work pool for reading from the DB.
	readPool threading.Pool,
	// A work pool for miscellaneous operations that are neither computationally intensive nor IO bound.
	miscPool threading.Pool,
) (SnapshotEngine, error) {
	if err := config.Validate(); err != nil {
		return nil, fmt.Errorf("invalid snapshot engine config: %w", err)
	}

	// TODO check if the DB is empty. If it's not, we should observe the initial hash.

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
	lifecycleBackpressureCond := sync.NewCond(versionLock)

	childCtx, cancel := context.WithCancel(ctx)

	c := &snapshotEngine{
		ctx:                       childCtx,
		cancel:                    cancel,
		config:                    config,
		shardManager:              shardManager,
		shards:                    shards,
		readPool:                  readPool,
		miscPool:                  miscPool,
		db:                        db,
		versionMap:                make(map[uint64]*snapshotReferenceCounter),
		currentVersion:            1, // important: versions start at 1, not 0, to allow version-1 without underflow
		oldestVersion:             1,
		versionLock:               versionLock,
		lifecycleBackpressureCond: lifecycleBackpressureCond,
		lifecycleWake:             make(chan struct{}, 1),
		iteratorRequests:          make(chan iteratorRequest),
		lifecycleExit:             make(chan struct{}, 1),
		lifecycleExited:           make(chan struct{}),
	}

	if config.MetricsEnabled {
		metrics := newSnapshotEngineMetrics(
			ctx, config.MetricsName, config.MetricsScrapeInterval(), c.getCacheSizeInfo)
		for _, s := range c.shards {
			s.metrics = metrics
		}
		c.metrics = metrics
	}

	go c.lifecycleRunner()

	return c, nil
}

func (c *snapshotEngine) getCacheSizeInfo() (bytes uint64, entries uint64) {
	for _, s := range c.shards {
		b, e := s.getSizeInfo()
		bytes += b
		entries += e
	}
	return bytes, entries
}

func (c *snapshotEngine) BatchSet(updates []Mutation) error {
	// Sort entries by shard index so each shard is locked only once.
	shardMap := make(map[uint64][]Mutation)
	for i := range updates {
		idx := c.shardManager.Shard(updates[i].Key)
		shardMap[idx] = append(shardMap[idx], updates[i])
	}

	var wg sync.WaitGroup
	for shardIndex, shardEntries := range shardMap {
		wg.Add(1)
		c.miscPool.Submit(func() {
			defer wg.Done()
			c.shards[shardIndex].BatchSet(shardEntries)
		})
	}
	wg.Wait()

	return nil
}

func (c *snapshotEngine) BatchGet(keys map[string]types.BatchGetResult) error {
	return c.BatchGetAtVersion(keys, c.currentVersion)
}

// Similar semantics to BatchGet, but reads from the given version of the engine.
func (c *snapshotEngine) BatchGetAtVersion(keys map[string]types.BatchGetResult, version uint64) error {
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

		c.miscPool.Submit(func() {
			defer wg.Done()
			err := c.shards[shardIndex].BatchGet(subMap, version)
			if err != nil {
				for key := range subMap {
					subMap[key] = types.BatchGetResult{Error: err}
				}
			}
		})
	}
	wg.Wait()

	for _, subMap := range work {
		for key, result := range subMap {
			keys[key] = result
		}
	}

	return nil
}

func (c *snapshotEngine) Delete(key []byte) {
	shardIndex := c.shardManager.Shard(key)
	shard := c.shards[shardIndex]
	shard.Delete(key)
}

// Similar semantics to Get, but reads from the given version of the engine.
func (c *snapshotEngine) Get(key []byte, updateLru bool) ([]byte, bool, error) {
	return c.GetAtVersion(key, c.currentVersion, updateLru)
}

func (c *snapshotEngine) GetAtVersion(key []byte, version uint64, updateLru bool) ([]byte, bool, error) {
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

func (c *snapshotEngine) Set(key []byte, value []byte) {
	shardIndex := c.shardManager.Shard(key)
	shard := c.shards[shardIndex]
	shard.Set(key, value)
}

func (c *snapshotEngine) Snapshot() (Snapshot, error) {
	c.metrics.setSnapshotPhase("acquire_version_lock")

	c.versionLock.Lock()
	defer c.versionLock.Unlock()

	c.metrics.setSnapshotPhase("lifecycle_backpressure")

	err := c.lifecycleBackpressureLocked()
	if err != nil {
		return nil, fmt.Errorf("lifecycle runner not keeping up: %w", err)
	}

	currentVersionRefCounter := &snapshotReferenceCounter{
		version:        c.currentVersion,
		referenceCount: 1,
		hashReady:      make(chan struct{}),
		flushCompleted: make(chan struct{}),
	}

	c.versionMap[c.currentVersion] = currentVersionRefCounter

	snapshot := &snapshotImpl{
		version:      c.currentVersion,
		parentEngine: c,
	}

	c.currentVersion++

	c.metrics.setSnapshotPhase("shards_snapshot")

	for _, shard := range c.shards {
		shardVersion := shard.Snapshot()
		if shardVersion != c.currentVersion {
			return nil, fmt.Errorf("shard (%d) has a different version than the engine (%d)",
				shardVersion, c.currentVersion)
		}
	}

	c.metrics.setSnapshotPhase("")

	return snapshot, nil
}

// This method blocks if the lifecycle runner is not keeping up. It is assumed that the caller already holds the
// versionLock. When this method returns, it will still hold the versionLock, but it may release and then
// re-acquire versionLock internally as it awaits for the lifecycle runner to catch up.
func (c *snapshotEngine) lifecycleBackpressureLocked() error {
	for c.unflushedCount > c.config.MaxUnretiredVersions {
		if c.ctx.Err() != nil {
			return fmt.Errorf("context cancelled")
		}
		c.lifecycleBackpressureCond.Wait()
	}
	// Re-check after the loop: a waiter may have been woken because a partial flush brought us back
	// below the cap, but if the lifecycle runner has since exited (cancelling the context), there is
	// no one to flush future work and the caller must not proceed.
	if c.ctx.Err() != nil {
		return fmt.Errorf("context cancelled")
	}
	return nil
}

// Increment the reference count for the given version.
func (c *snapshotEngine) IncrementReferenceCount(version uint64) error {
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
		// Should be impossible since version retirement never leaves gaps
		return fmt.Errorf("version (%d) not found", version)
	}

	if counter.referenceCount == 0 {
		return fmt.Errorf("version (%d) has already been dropped", version)
	}

	counter.referenceCount++
	return nil
}

// Decrement the reference count for the given version.
func (c *snapshotEngine) DecrementReferenceCount(version uint64) error {
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
		// Should be impossible since version retirement never leaves gaps
		return fmt.Errorf("version (%d) not found", version)
	}

	if counter.referenceCount == 0 {
		return fmt.Errorf("version (%d) has already been dropped", version)
	}

	if counter.referenceCount == 1 && counter.hash == nil {
		return fmt.Errorf("version (%d) was fully released without first being hashed", version)
	}

	counter.referenceCount--

	c.maybeWakeLifecycleLocked()

	return nil
}

// Scans for new flush eligible versions. Returns true if there is a new flush eligible version discovered.
//
// The Locked postfix indicates that the caller must hold the versionLock.
func (c *snapshotEngine) scanForFlushEligibilityLocked() bool {
	oldHighestFlushEligibleVersion := c.highestFlushEligibleVersion

	// Clip the start to oldestVersion: prior retirements may have advanced oldestVersion past the
	// stale oldHighestFlushEligibleVersion, leaving any versions in between absent from versionMap.
	start := oldHighestFlushEligibleVersion + 1
	if start < c.oldestVersion {
		start = c.oldestVersion
	}

	for version := start; version < c.currentVersion; version++ {
		counter := c.versionMap[version]

		if counter.hash == nil {
			// We can only flush hashed snapshots. If we hit an unhashed one, no more flushing is possible.
			break
		}

		if !counter.flushedToDisk {
			// This snapshot is flush eligible.
			c.unflushedCount++
			c.highestFlushEligibleVersion = version
		}

		if counter.referenceCount > 0 {
			// The first snapshot that is not fully released is flush eligible, but all following
			// snapshots are not.
			break
		}
	}

	return c.highestFlushEligibleVersion > oldHighestFlushEligibleVersion
}

// Scans for new retirement eligible versions. Returns true if there is a new retirement eligible version discovered.
//
// The Locked postfix indicates that the caller must hold the versionLock.
func (c *snapshotEngine) scanForRetirementEligibilityLocked() bool {
	oldHighestRetirementEligibleVersion := c.highestRetirementEligibleVersion

	// Clip the start to oldestVersion: prior retirements may have advanced oldestVersion past the
	// stale oldHighestRetirementEligibleVersion, leaving any versions in between absent from versionMap.
	start := oldHighestRetirementEligibleVersion + 1
	if start < c.oldestVersion {
		start = c.oldestVersion
	}

	for version := start; version < c.currentVersion; version++ {
		counter := c.versionMap[version]
		if counter.referenceCount != 0 || !counter.flushedToDisk {
			// We can only retire snapshots that are fully released and flushed to disk.
			break
		}
		c.highestRetirementEligibleVersion = version
	}

	return c.highestRetirementEligibleVersion > oldHighestRetirementEligibleVersion
}

// Determine if we need to wake up the lifecycle runner to do work (either flushing or retiring snapshots).
//
// The Locked postfix indicates that the caller must hold the versionLock.
func (c *snapshotEngine) maybeWakeLifecycleLocked() {
	newFlushEligible := c.scanForFlushEligibilityLocked()
	newRetirementEligible := c.scanForRetirementEligibilityLocked()
	if newFlushEligible || newRetirementEligible {
		select {
		case c.lifecycleWake <- struct{}{}:
		default:
			// If the channel is full then the lifecycle runner will already have a pending wakeup call.
		}
	}
}

// SetSnapshotHash attaches a hash to the snapshot at the given version.
func (c *snapshotEngine) SetSnapshotHash(version uint64, hash []byte) error {
	if hash == nil {
		return fmt.Errorf("hash cannot be nil")
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
	close(counter.hashReady)

	c.maybeWakeLifecycleLocked()

	return nil
}

// AwaitSnapshotHash blocks until the hash for the given version is available.
func (c *snapshotEngine) AwaitSnapshotHash(ctx context.Context, version uint64) ([]byte, error) {
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

	select {
	case <-hashReady:
		// hashReady is closed when the hash is set, causing us to get a nil from <-hashReady.
	case <-ctx.Done():
		return nil, fmt.Errorf("failed to await hash: %w", ctx.Err())
	}

	return counter.hash, nil
}

// isVersionFlushed briefly takes versionLock to check the current flush state of the given version.
// The caller must NOT hold versionLock. A retired version (counter no longer in versionMap) is treated
// as flushed, since retirement requires flushedToDisk == true.
func (c *snapshotEngine) isVersionFlushed(version uint64) bool {
	c.versionLock.Lock()
	defer c.versionLock.Unlock()
	counter, ok := c.versionMap[version]
	if !ok {
		return true
	}
	return counter.flushedToDisk
}

// Get the diff at a given version.
func (c *snapshotEngine) GetDiffAtVersion(version uint64) (map[string][]byte, error) {
	diff := make(map[string][]byte)

	for _, shard := range c.shards {
		shardDiff, err := shard.GetDiffsForVersions(version, version+1)
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

// requestIterator dispatches an iterator request to the lifecycle goroutine
// and blocks until the iterator is built. The caller (typically
// snapshotImpl.Iterator) is responsible for holding a reservation on version
// across this call so the lifecycle goroutine can't retire it out from under
// us. See iteratorRequest for why construction is routed through the
// lifecycle goroutine.
func (c *snapshotEngine) requestIterator(version uint64) (Iterator, error) {
	req := iteratorRequest{
		version:  version,
		response: make(chan iteratorResponse, 1),
	}
	select {
	case c.iteratorRequests <- req:
	case <-c.ctx.Done():
		return nil, fmt.Errorf(
			"snapshot engine shut down before iterator request could be dispatched: %w", c.ctx.Err())
	}
	select {
	case resp := <-req.response:
		return resp.iter, resp.err
	case <-c.ctx.Done():
		return nil, fmt.Errorf("snapshot engine shut down before iterator could be built: %w", c.ctx.Err())
	}
}

// buildIterator constructs an Iterator over the snapshot at version. Must run
// on the lifecycle goroutine — see iteratorRequest for why.
//
// Order matters: we materialize the in-memory overrides BEFORE opening the
// DB iterator. The reverse order would let a concurrent flush+retire move
// data from versionedData into the DB *after* dbIter's point-in-time snapshot
// is taken, dropping those keys entirely. (Lifecycle-goroutine serialization
// already prevents this race today, but the order is a cheap belt to go with
// the suspenders.)
//
// The returned iterator exposes everything in the snapshot, including the
// engine's metadata hash key (see flushSnapshots). Callers like state sync
// rely on observing the hash alongside user data.
func (c *snapshotEngine) buildIterator(version uint64) (Iterator, error) {
	overrides, err := c.materializeOverridesAtVersion(version)
	if err != nil {
		return nil, fmt.Errorf("failed to materialize overrides at version (%d): %w", version, err)
	}

	dbIter, err := c.db.NewIter(nil)
	if err != nil {
		return nil, fmt.Errorf("failed to create db iterator: %w", err)
	}

	iter, err := newSnapshotIterator(overrides, dbIter)
	if err != nil {
		return nil, fmt.Errorf("failed to create snapshot iterator: %w", err)
	}

	return iter, nil
}

// materializeOverridesAtVersion gathers in-memory overrides visible at
// version from every shard and returns them sorted ascending by key. Each
// shard is responsible for its own locking; here we just stitch the results
// together.
func (c *snapshotEngine) materializeOverridesAtVersion(version uint64) ([]kvPair, error) {
	var all []kvPair
	for i, s := range c.shards {
		shardOverrides, err := s.materializeOverridesAtVersion(version)
		if err != nil {
			return nil, fmt.Errorf("shard %d: %w", i, err)
		}
		all = append(all, shardOverrides...)
	}
	sort.Slice(all, func(i, j int) bool {
		return bytes.Compare(all[i].key, all[j].key) < 0
	})
	return all, nil
}

// Retire old versions: flush their data to the underlying DB, then free the in-memory snapshots.
// The runner sleeps until signaled via lifecycleWake (see wakeLifecycle / scanRetirementEligibility),
// then processes all available work before sleeping again.
func (c *snapshotEngine) lifecycleRunner() {
	// Once the runner exits — whether normally, via context cancellation, or via panic — wake up any
	// goroutines blocked in lifecycleBackpressureLocked so they can observe the cancelled context and
	// return errors instead of waiting forever for a lifecycle that is no longer running.
	defer func() {
		c.cancel()
		c.lifecycleBackpressureCond.Broadcast()
		close(c.lifecycleExited)
	}()

	for {
		select {
		case <-c.lifecycleExit:
			return
		case <-c.ctx.Done():
			return
		case req := <-c.iteratorRequests:
			// Build the iterator inline. The single-threaded lifecycle goroutine
			// guarantees no concurrent flush or retire can race with the
			// materialization, which is the whole point of routing through here.
			iter, err := c.buildIterator(req.version)
			req.response <- iteratorResponse{iter: iter, err: err}
			continue
		case <-c.lifecycleWake:
		}

		err := c.doLifecycleWork()
		if err != nil {
			panic(err)
		}
	}
}

// Flushes and retires snapshots. Continues running until there is no more work, then returns.
func (c *snapshotEngine) doLifecycleWork() error {
	hasWork := true

	for hasWork {

		c.versionLock.Lock()

		firstFlushVersion, lastFlushVersion, versionHashes, err := c.determineVersionsToFlushLocked()
		if err != nil {
			c.versionLock.Unlock()
			return fmt.Errorf("unable to determine versions to flush: %w", err)
		}

		firstRetireVersion, lastRetireVersion := c.determineVersionsToRetireLocked()

		unflushedCount := lastFlushVersion - firstFlushVersion
		unretiredCount := lastRetireVersion - firstRetireVersion
		hasWork = unflushedCount > 0 || unretiredCount > 0

		c.versionLock.Unlock()

		err = c.flushSnapshots(firstFlushVersion, lastFlushVersion, versionHashes)
		if err != nil {
			return fmt.Errorf("unable to flush snapshots: %w", err)
		}

		err = c.retireSnapshots(firstRetireVersion, lastRetireVersion)
		if err != nil {
			return fmt.Errorf("unable to retire snapshots: %w", err)
		}
	}

	return nil
}

// Determine which versions need to be flushed to disk.
func (c *snapshotEngine) determineVersionsToFlushLocked() (
	// The first version to be flushed, inclusive.
	firstVersion uint64,
	// The last version to be flushed, exclusive.
	lastVersion uint64,
	// The hashes of the versions to be flushed.
	versionHashes map[uint64][]byte,
	err error,
) {

	firstVersion = c.oldestVersion
	lastVersion = c.oldestVersion
	versionHashes = make(map[uint64][]byte)

	if c.oldestVersion == c.currentVersion {
		// The only version we are tracking is the mutable version, which is never flush eligible.
		return
	}

	// If the oldest tracked version has already been flushed, then there is a pending retirement
	// that hasn't run yet.
	if c.versionMap[c.oldestVersion].flushedToDisk {
		return
	}

	for targetVersion := firstVersion; targetVersion < c.currentVersion; targetVersion++ {
		counter := c.versionMap[targetVersion]

		if counter.hash == nil {
			// Unhashed snapshots are not flush eligible.
			break
		}

		// Mark the current snapshot as flush eligible.
		versionHashes[targetVersion] = counter.hash
		lastVersion++

		if counter.referenceCount > 0 {
			// We've encountered a snapshot that is not retirement eligible. Although this snapshot
			// can be safely flushed, all later snapshots must wait on this non-retired snapshot.
			break
		}
	}

	return firstVersion, lastVersion, versionHashes, nil
}

// Determine which versions need to be retired.
func (c *snapshotEngine) determineVersionsToRetireLocked() (
	// The first version to be retired, inclusive.
	firstVersion uint64,
	// The last version to be retired, exclusive.
	lastVersion uint64,
) {

	firstVersion = c.oldestVersion
	lastVersion = c.oldestVersion

	for targetVersion := c.oldestVersion; targetVersion < c.currentVersion; targetVersion++ {
		counter := c.versionMap[targetVersion]

		if counter.referenceCount > 0 || !counter.flushedToDisk {
			// We can only retire versions that are entirely released and flushed to disk.
			break
		}

		// Include this snapshot in the set to be retired
		lastVersion++
	}

	return firstVersion, lastVersion
}

// flushSnapshots collects diffs for [firstVersion, lastVersion) from all shards,
// writes them to the underlying DB in batches, then drops the versions from the shards.
func (c *snapshotEngine) flushSnapshots(
	// The first version to flush (inclusive).
	firstVersion uint64,
	// The last version to flush (exclusive).
	lastVersion uint64,
	// The hash of each version to flush.
	versionHashes map[uint64][]byte,
) error {

	// Collect diffs from all shards.
	diffsByVersion := make(map[uint64]map[string][]byte)
	for version := firstVersion; version < lastVersion; version++ {
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

	// For each version, append the metadata hash key so that it is written to the DB atomically with
	// its block's diff.
	for version := firstVersion; version < lastVersion; version++ {
		diffsByVersion[version][c.config.HashKey] = versionHashes[version]
	}

	// Write diffs to the DB in batches, oldest version first.
	var batch types.Batch
	versionsInBatch := uint64(0)
	for version := firstVersion; version < lastVersion; version++ {
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
			if err := batch.Commit(types.WriteOptions{Sync: false}); err != nil { // TODO check sync requirement
				return fmt.Errorf("flush failed to commit batch: %w", err)
			}
			batch = nil
			c.versionLock.Lock()
			c.unflushedCount -= versionsInBatch
			c.versionLock.Unlock()
			c.lifecycleBackpressureCond.Signal()
			versionsInBatch = 0
		}
	}
	if batch != nil {
		if err := batch.Commit(types.WriteOptions{Sync: false}); err != nil { // TODO check sync requirement
			return fmt.Errorf("flush failed to commit batch: %w", err)
		}
		c.versionLock.Lock()
		c.unflushedCount -= versionsInBatch
		c.versionLock.Unlock()
		c.lifecycleBackpressureCond.Signal()
	}

	// Mark the flushed versions as having been flushed.
	c.versionLock.Lock()
	for version := firstVersion; version < lastVersion; version++ {
		counter := c.versionMap[version]
		counter.flushedToDisk = true
		close(counter.flushCompleted)
	}
	c.versionLock.Unlock()

	return nil
}

// Retire all eligible snapshots.
func (c *snapshotEngine) retireSnapshots(
	// The first version to retire (inclusive).
	firstVersion uint64,
	// The last version to retire (exclusive).
	lastVersion uint64,
) error {

	if firstVersion >= lastVersion {
		// Nothing to retire. Callers in doLifecycleWork pass the same first/last when
		// no version is retirement-eligible.
		return nil
	}

	for i, shard := range c.shards {
		err := shard.DropVersions(firstVersion, lastVersion)
		if err != nil {
			return fmt.Errorf("failed to drop versions from shard %d %w", i, err)
		}
	}

	c.versionLock.Lock()
	defer c.versionLock.Unlock()

	for targetVersion := firstVersion; targetVersion < lastVersion; targetVersion++ {
		counter := c.versionMap[targetVersion]
		if counter.referenceCount > 0 || !counter.flushedToDisk {
			// Sanity check, should be impossible
			return fmt.Errorf("expected snapshot to be released and flushed, refcount = %d, flushed = %t",
				counter.referenceCount, counter.flushedToDisk)
		}
		delete(c.versionMap, targetVersion)
	}
	c.oldestVersion = lastVersion

	return nil
}

// UnderlyingDB returns the raw backing database. Intended for test-only use
// (e.g. iteration for ground-truth verification). Production code should use
// the SnapshotEngine interface methods.
func (c *snapshotEngine) UnderlyingDB() types.KeyValueDB {
	return c.db
}

func (c *snapshotEngine) Close() error {
	// TODO make this method idempotent

	exit := func() error {
		c.cancel()

		// wake the backpressure thread, just in case it is blocked.
		// It will exit when it sees that the context is cancelled.
		c.lifecycleBackpressureCond.Signal()

		err := c.db.Close()
		if err != nil {
			return fmt.Errorf("close: failed to close database: %w", err)
		}

		return nil
	}

	// Request that the lifecycle runner exit.
	select {
	case c.lifecycleExit <- struct{}{}:
	case <-c.ctx.Done():
		err := fmt.Errorf("context cancelled: %w", c.ctx.Err())
		exitErr := exit()
		return errors.Join(err, exitErr)
	}

	// Wait for the lifecycle runner to exit. We deliberately do NOT race ctx.Done() here:
	// lifecycle's defer cancels c.ctx on exit, so a select with ctx.Done() could win over the
	// lifecycleExited close non-deterministically. Lifecycle is guaranteed to eventually close
	// lifecycleExited (its defer runs even on panic), so this is a bounded wait.
	<-c.lifecycleExited

	// Flush whatever is immediately flush-eligible by the normal rules. We don't wait on
	// unhashed snapshots — Close should not block on the hashing subsystem. Retirement is
	// also skipped: it only frees in-memory state, which the GC will reclaim once the engine
	// reference is dropped.
	c.versionLock.Lock()
	firstVersion, lastVersion, versionHashes, err := c.determineVersionsToFlushLocked()
	c.versionLock.Unlock()
	if err != nil {
		err = fmt.Errorf("close: failed to determine versions to flush: %w", err)
		exitErr := exit()
		return errors.Join(err, exitErr)
	}

	if firstVersion < lastVersion {
		if err := c.flushSnapshots(firstVersion, lastVersion, versionHashes); err != nil {
			err := fmt.Errorf("close: failed to flush versions: %w", err)
			exitErr := exit()
			return errors.Join(err, exitErr)
		}
	}

	return exit()
}
