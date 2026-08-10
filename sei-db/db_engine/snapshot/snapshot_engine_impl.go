package snapshot

import (
	"bytes"
	"context"
	"fmt"
	"sort"
	"sync"

	dbm "github.com/tendermint/tm-db"

	"github.com/sei-protocol/sei-chain/sei-db/common/threading"
	"github.com/sei-protocol/sei-chain/sei-db/db_engine/types"
	"github.com/sei-protocol/sei-chain/sei-db/proto"
)

var _ SnapshotEngine = (*snapshotEngine)(nil)

// The standard implementation of SnapshotEngine.
type snapshotEngine struct {
	// Engine-private context. Blocked waits throughout the engine select on it; it is cancelled
	// (always under versionLock — see closeInternal) when the engine shuts down, via Close or a
	// fatal lifecycle error.
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
	//
	// Written only while holding versionLock (by Commit()). Read without the lock by the
	// Get/BatchGet paths; this is sound only because the engine contract forbids calling
	// Commit() concurrently with operations on the current version, so no lockless read can
	// race the write.
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

	// The number of snapshots that are eligible to be flushed to disk, but which have not yet been
	// flushed. Versions blocked behind a still-referenced snapshot are not counted until that
	// snapshot is released (see scanForFlushEligibilityLocked), so this measures work the
	// underlying DB can actually perform — Commit backpressure driven by it reflects DB lag, not
	// release lag.
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

	// Used to enforce lifecycle backpressure. We want to block Commit() if unretiredVersions grows too large.
	lifecycleBackpressureCond *sync.Cond

	// Signaled (non-blocking) by scanRetirementEligibility when new versions become
	// eligible for retirement. The lifecycle runner selects on this to wake up.
	lifecycleWake chan struct{}

	// A struct{} is sent on this channel when the lifecycle runner should exit. The lifecycle goroutine
	// signals that it has exited by closing lifecycleExited (below), which Close waits on.
	lifecycleExit chan struct{}

	// Closed by the lifecycle goroutine immediately before it returns. Close waits on this to ensure
	// the lifecycle runner has fully stopped before performing its final flush.
	lifecycleExited chan struct{}

	// Metrics for recording snapshot engine statistics.
	metrics *SnapshotEngineMetrics

	// The error that bricked the engine, latched by the lifecycle runner when lifecycle work
	// (e.g. a flush) fails. Once set it is never cleared: the lifecycle exits, the engine context
	// is cancelled, and methods that observe the shutdown report this error. Nil if the engine
	// has not failed.
	//
	// Protected by versionLock.
	fatalErr error

	// Ensures Close runs its teardown exactly once; closeErr memoizes the result so repeat calls
	// return the same error without re-running teardown.
	closeOnce sync.Once
	closeErr  error
}

// Tracks the reference count for a particular snapshot.
type snapshotReferenceCounter struct {
	// The version/block height of the snapshot.
	version uint64

	// The number of reservations currently held for this snapshot. When this count reaches 0,
	// the snapshot is eligible for retirement.
	referenceCount uint64

	// True once FinalizeSnapshot has run for this snapshot. Flushing is gated on it. By contract the
	// finalizing consumer holds a reservation until it has finalized, so this is guaranteed to be
	// true before referenceCount reaches 0. Enforced in DecrementReferenceCount.
	finalized bool

	// The metadata pairs supplied to FinalizeSnapshot, written to disk in the same atomic batch as
	// this snapshot's diff. May be empty: a consumer with nothing to record still finalizes.
	finalWrites []*proto.KVPair

	// True if the snapshot has been flushed, otherwise false.
	flushedToDisk bool

	// Closed by flushSnapshots when this snapshot's data has been persisted to disk. Pairs with
	// flushedToDisk. Used as a synchronization handle for AwaitFlush waiters; a closed channel is
	// immediately selectable, so the "already flushed at call time" case requires no special path.
	flushCompleted chan struct{}
}

// Creates a new SnapshotEngine.
//
// The engine takes ownership of db and closes it in Close. Nothing else may read or write that database
// afterwards: doing so bypasses the engine's staging and cache and sees or corrupts a version nobody
// asked for. The pools, by contrast, are shared and remain the caller's to close — after the engine,
// since the engine's goroutines submit to them.
func NewSnapshotEngine(
	config *SnapshotEngineConfig,
	// The underlying key-value database.
	db types.KeyValueDB,
	// A work pool for reading from the DB.
	readPool threading.Pool,
	// A work pool for miscellaneous operations that are neither computationally intensive nor IO bound.
	// Must not be the same bounded pool as readPool: tasks submitted here block waiting on
	// readPool results, so sharing one fixed-size pool can deadlock under load. Pass distinct
	// pools, or an elastic pool.
	miscPool threading.Pool,
) (SnapshotEngine, error) {
	if err := config.Validate(); err != nil {
		return nil, fmt.Errorf("invalid snapshot engine config: %w", err)
	}

	shardManager, err := newShardManager(config.ShardCount)
	if err != nil {
		return nil, fmt.Errorf("failed to create shard manager: %w", err)
	}
	sizePerShard := config.MaxSize / config.ShardCount

	// Everything owned by the engine (shards, the metrics scrape loop, blocked waits) lives on
	// this engine-private context, cancelled only when the engine shuts down.
	childCtx, cancel := context.WithCancel(context.Background())

	versionLock := &sync.Mutex{}
	lifecycleBackpressureCond := sync.NewCond(versionLock)

	c := &snapshotEngine{
		ctx:          childCtx,
		cancel:       cancel,
		config:       config,
		shardManager: shardManager,
		readPool:     readPool,
		miscPool:     miscPool,
		db:           db,
		versionMap:   make(map[uint64]*snapshotReferenceCounter),
		// Versions start at 1 (not 0) so a version-1 lookup never underflows.
		currentVersion:            1,
		oldestVersion:             1,
		versionLock:               versionLock,
		lifecycleBackpressureCond: lifecycleBackpressureCond,
		lifecycleWake:             make(chan struct{}, 1),
		lifecycleExit:             make(chan struct{}, 1),
		lifecycleExited:           make(chan struct{}),
	}

	// Shards are created after the engine struct so they can report the engine's shutdown error
	// (the latched fatal error, or ErrEngineClosed) when a blocked read is released by context
	// cancellation — see the Close contract on SnapshotEngine.
	shards := make([]*shard, config.ShardCount)
	for i := uint64(0); i < config.ShardCount; i++ {
		shards[i], err = NewShard(
			childCtx, config, db, readPool, sizePerShard, c.shutdownError, c.reportReadFailure)
		if err != nil {
			cancel()
			return nil, fmt.Errorf("failed to create shard: %w", err)
		}
	}
	c.shards = shards

	if config.MetricsEnabled {
		metrics := newSnapshotEngineMetrics(
			childCtx, config.Name, config.MetricsScrapeInterval(), c.getCacheSizeInfo)
		for _, s := range c.shards {
			s.metrics = metrics
			s.cache.metrics = metrics
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

func (c *snapshotEngine) BatchSet(updates []*proto.KVPair) error {
	// Sort entries by shard index so each shard is locked only once.
	shardMap := make(map[uint64][]*proto.KVPair)
	for i := range updates {
		idx := c.shardManager.Shard(updates[i].Key)
		shardMap[idx] = append(shardMap[idx], updates[i])
	}

	// Fan out to shards. A shard refusing the write (an iterator is open) fails the whole call; the
	// shards that accepted it have already applied their entries, so the batch is not atomic across
	// shards in that case. That is acceptable because it can only happen on caller misuse, and the
	// engine contract makes any error fatal.
	var wg sync.WaitGroup
	shardIndices := make([]uint64, 0, len(shardMap))
	for shardIndex := range shardMap {
		shardIndices = append(shardIndices, shardIndex)
	}
	errs := make([]error, len(shardIndices))
	for i, shardIndex := range shardIndices {
		wg.Add(1)
		c.miscPool.Submit(func() {
			defer wg.Done()
			errs[i] = c.shards[shardIndex].BatchSet(shardMap[shardIndex])
		})
	}
	wg.Wait()

	for i := range errs {
		if errs[i] != nil {
			return fmt.Errorf("failed to batch set in shard: %w", errs[i])
		}
	}
	return nil
}

func (c *snapshotEngine) BatchGet(keys [][]byte) (map[string][]byte, error) {
	return c.BatchGetAtVersion(keys, c.currentVersion)
}

// Similar semantics to BatchGet, but reads from the given version of the engine.
func (c *snapshotEngine) BatchGetAtVersion(keys [][]byte, version uint64) (map[string][]byte, error) {
	// Partition the keys by shard so each shard is queried once.
	work := make(map[uint64][][]byte)
	for _, key := range keys {
		shardIndex := c.shardManager.Shard(key)
		work[shardIndex] = append(work[shardIndex], key)
	}

	// Fan out to shards, collecting each shard's found results (or its error).
	shardIndices := make([]uint64, 0, len(work))
	for shardIndex := range work {
		shardIndices = append(shardIndices, shardIndex)
	}
	results := make([]map[string][]byte, len(shardIndices))
	errs := make([]error, len(shardIndices))

	var wg sync.WaitGroup
	for i, shardIndex := range shardIndices {
		wg.Add(1)
		c.miscPool.Submit(func() {
			defer wg.Done()
			results[i], errs[i] = c.shards[shardIndex].BatchGet(work[shardIndex], version)
		})
	}
	wg.Wait()

	// Merge into a single result map. Any shard error fails the whole call.
	merged := make(map[string][]byte, len(keys))
	for i := range results {
		if errs[i] != nil {
			return nil, fmt.Errorf("failed to batch get from shard: %w", errs[i])
		}
		for key, value := range results[i] {
			merged[key] = value
		}
	}
	return merged, nil
}

func (c *snapshotEngine) Delete(key []byte) error {
	shardIndex := c.shardManager.Shard(key)
	shard := c.shards[shardIndex]
	if err := shard.Delete(key); err != nil {
		return fmt.Errorf("failed to delete key in shard: %w", err)
	}
	return nil
}

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

func (c *snapshotEngine) Set(key []byte, value []byte) error {
	shardIndex := c.shardManager.Shard(key)
	shard := c.shards[shardIndex]
	if err := shard.Set(key, value); err != nil {
		return fmt.Errorf("failed to set key in shard: %w", err)
	}
	return nil
}

func (c *snapshotEngine) Commit() (Snapshot, error) {
	c.metrics.setSnapshotPhase("acquire_version_lock")
	// Reset the phase on every exit so error returns don't leave the timer stuck on a phase.
	defer c.metrics.setSnapshotPhase("")

	c.versionLock.Lock()
	defer c.versionLock.Unlock()

	// A bricked engine does no more work. Free to check here: versionLock, which guards fatalErr,
	// is already held.
	if c.fatalErr != nil {
		return nil, fmt.Errorf("cannot create snapshot: %w", c.shutdownErrorLocked())
	}

	// Sealing the version under an open iterator is refused for the same reason writes are: the
	// iterator's view must not shift beneath it.
	for i, s := range c.shards {
		s.lock.Lock()
		err := s.writableLocked()
		s.lock.Unlock()
		if err != nil {
			return nil, fmt.Errorf("cannot create snapshot, shard %d: %w", i, err)
		}
	}

	c.metrics.setSnapshotPhase("lifecycle_backpressure")

	err := c.lifecycleBackpressureLocked()
	if err != nil {
		return nil, fmt.Errorf("cannot create snapshot: %w", err)
	}

	currentVersionRefCounter := &snapshotReferenceCounter{
		version:        c.currentVersion,
		referenceCount: 1,
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
		shardVersion := shard.Commit()
		if shardVersion != c.currentVersion {
			// Should be impossible. The engine is now inconsistent (some shards committed, some
			// not), so brick it: the failure must be latched and every subsequent call must fail,
			// rather than leaving the engine callable after a fatal error.
			err := fmt.Errorf("shard (%d) has a different version than the engine (%d)",
				shardVersion, c.currentVersion)
			c.brickLocked(err)
			return nil, err
		}
	}

	return snapshot, nil
}

// This method blocks if the lifecycle runner is not keeping up. It is assumed that the caller already holds the
// versionLock. When this method returns, it will still hold the versionLock, but it may release and then
// re-acquire versionLock internally as it awaits for the lifecycle runner to catch up.
func (c *snapshotEngine) lifecycleBackpressureLocked() error {
	for c.unflushedCount > c.config.MaxUnflushedVersions {
		if c.ctx.Err() != nil {
			return c.shutdownErrorLocked()
		}
		c.lifecycleBackpressureCond.Wait()
	}
	// Re-check after the loop: a waiter may have been woken because a partial flush brought us back
	// below the cap, but if the lifecycle runner has since exited (cancelling the context), there is
	// no one to flush future work and the caller must not proceed.
	if c.ctx.Err() != nil {
		return c.shutdownErrorLocked()
	}
	return nil
}

// shutdownErrorLocked builds the error reported by methods that observe engine shutdown: the
// latched fatal error when the lifecycle runner failed, otherwise ErrEngineClosed.
//
// The Locked postfix indicates that the caller must hold the versionLock.
func (c *snapshotEngine) shutdownErrorLocked() error {
	if c.fatalErr != nil {
		return fmt.Errorf("snapshot engine failed: %w", c.fatalErr)
	}
	return ErrEngineClosed
}

// shutdownError is the unlocked variant of shutdownErrorLocked. The caller must NOT hold versionLock.
func (c *snapshotEngine) shutdownError() error {
	c.versionLock.Lock()
	defer c.versionLock.Unlock()
	return c.shutdownErrorLocked()
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

	if counter.referenceCount == 1 && !counter.finalized {
		// Releasing the last reservation without finalizing is fatal (see the finalization-duty
		// contract on Snapshot). This snapshot can now never make progress:
		// determineVersionsToFlushLocked skips unfinalized versions, retirement requires a flush, and
		// finalization will never arrive because the caller has spent its Release. Every later version
		// stalls behind it with its in-memory data accumulating. Note the reference count is not what
		// traps it — decrementing would not help.
		//
		// The sibling errors in this method deliberately do not brick: those reject a bogus version
		// and leave engine state untouched, so that caller can retry with the right one.
		err := fmt.Errorf("version (%d) was fully released without first being finalized", version)
		c.brickLocked(err)
		return err
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

	if counter, ok := c.versionMap[oldHighestFlushEligibleVersion]; ok && counter.referenceCount > 0 {
		// The version at the watermark is still referenced, so nothing past it can be flushed
		// until it is released and retired.
		return false
	}

	// Clip the start to oldestVersion: prior retirements may have advanced oldestVersion past the
	// stale oldHighestFlushEligibleVersion, leaving any versions in between absent from versionMap.
	start := oldHighestFlushEligibleVersion + 1
	if start < c.oldestVersion {
		start = c.oldestVersion
	}

	for version := start; version < c.currentVersion; version++ {
		counter := c.versionMap[version]

		if !counter.finalized {
			// We can only flush finalized snapshots. If we hit an unfinalized one, no more flushing is
			// possible.
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

// FinalizeSnapshot attaches metadata writes to the snapshot at the given version and makes it
// eligible to be flushed. An empty write set is legal.
func (c *snapshotEngine) FinalizeSnapshot(version uint64, writes []*proto.KVPair) error {
	c.versionLock.Lock()
	defer c.versionLock.Unlock()

	counter, ok := c.versionMap[version]
	if !ok {
		return fmt.Errorf("version (%d) not found", version)
	}

	if counter.finalized {
		return fmt.Errorf("version %d has already been finalized", version)
	}

	counter.finalized = true
	counter.finalWrites = writes

	c.maybeWakeLifecycleLocked()

	return nil
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

func (c *snapshotEngine) Iterator(opts *types.IterOptions) (dbm.Iterator, error) {
	// Overrides first, DB iterator second, and the order is load-bearing: a concurrent flush+retire
	// that moved data out of versionedData and into the DB between the two steps would drop those
	// keys entirely if the DB snapshot were taken first. In this order the same race can only yield a
	// key twice, which the merge resolves in favor of the override.
	overrides, err := c.materializeCurrentOverrides(opts)
	if err != nil {
		return nil, fmt.Errorf("failed to materialize current overrides: %w", err)
	}

	dbIter, err := c.db.NewIter(opts)
	if err != nil {
		return nil, fmt.Errorf("failed to create db iterator: %w", err)
	}

	var lowerBound, upperBound []byte
	reverse := false
	if opts != nil {
		lowerBound, upperBound, reverse = opts.LowerBound, opts.UpperBound, opts.Reverse
	}
	iter, err := newSnapshotIterator(
		overrides, dbIter, []byte(c.config.ReservedPrefix), reverse, lowerBound, upperBound)
	if err != nil {
		return nil, fmt.Errorf("failed to create snapshot iterator: %w", err)
	}

	// Block writes only now that construction has fully succeeded, so a failed construction cannot
	// leave the engine permanently unwritable.
	for _, s := range c.shards {
		s.iteratorOpened()
	}
	return &writeBlockingIterator{Iterator: iter, engine: c}, nil
}

// writeBlockingIterator releases the engine's write block when the underlying iterator is closed.
// Close is idempotent, so the release happens exactly once no matter how often it is called.
type writeBlockingIterator struct {
	dbm.Iterator
	engine *snapshotEngine
	closed bool
}

func (w *writeBlockingIterator) Close() error {
	if w.closed {
		return nil
	}
	w.closed = true
	for _, s := range w.engine.shards {
		s.iteratorClosed()
	}
	return w.Iterator.Close()
}

// materializeCurrentOverrides gathers the in-memory overrides at the current version from every
// shard and returns them sorted ascending by key. Each shard is responsible for its own locking;
// here we just stitch the results together, and the sort runs without any shard lock held.
// The overrides are sorted into iteration order — ascending, or descending when reverse is set — so
// the merge in snapshotIterator can walk them and the DB iterator in lockstep.
func (c *snapshotEngine) materializeCurrentOverrides(opts *types.IterOptions) ([]kvPair, error) {
	var lowerBound, upperBound []byte
	reverse := false
	if opts != nil {
		lowerBound, upperBound, reverse = opts.LowerBound, opts.UpperBound, opts.Reverse
	}

	var all []kvPair
	for i, s := range c.shards {
		shardOverrides, err := s.materializeCurrentOverrides(lowerBound, upperBound)
		if err != nil {
			return nil, fmt.Errorf("shard %d: %w", i, err)
		}
		all = append(all, shardOverrides...)
	}
	sort.Slice(all, func(i, j int) bool {
		cmp := bytes.Compare(all[i].key, all[j].key)
		if reverse {
			return cmp > 0
		}
		return cmp < 0
	})
	return all, nil
}

// Retire old versions: flush their data to the underlying DB, then free the in-memory snapshots.
// The runner sleeps until signaled via lifecycleWake (see wakeLifecycle / scanRetirementEligibility),
// then processes all available work before sleeping again. It exits only via its lifecycleExit
// inbox (sent by Close) or by bricking on a fatal error.
func (c *snapshotEngine) lifecycleRunner() {
	defer close(c.lifecycleExited)

	for {
		select {
		case <-c.lifecycleExit:
			return
		case <-c.ctx.Done():
			// The engine was bricked from outside the runner (see Commit): the version state is
			// no longer trustworthy, so stop doing lifecycle work. Close cancels the context only
			// after this goroutine has exited, so this case never fires on a clean shutdown.
			return
		case <-c.lifecycleWake:
		}

		err := c.doLifecycleWork()
		if err != nil {
			// Brick the engine and exit. The caller is expected to halt the node on the
			// first error it sees.
			c.brick(err)
			return
		}
	}
}

// brick latches the fatal error and releases everyone blocked on the engine, so callers observe
// the failure immediately rather than waiting for Close.
func (c *snapshotEngine) brick(err error) {
	c.versionLock.Lock()
	c.brickLocked(err)
	c.versionLock.Unlock()
}

// reportReadFailure handles a failed DB read by bricking the engine.
//
// Halting is the caller's responsibility, not ours (see the SnapshotEngine doc). This exists so that
// a caller which ignores the failure gets nowhere, not to police it: reads racing this call may still
// succeed, and no promise is made about when they stop.
//
// Must be called without the shard lock held: it acquires versionLock, and the established order is
// versionLock before any shard lock.
func (c *snapshotEngine) reportReadFailure(err error) {
	c.brick(fmt.Errorf("failed to read from the underlying database: %w", err))
}

// brickLocked latches the fatal error, cancels the engine context, wakes backpressure waiters, and
// takes every shard out of service, so callers observe the failure immediately rather than waiting
// for Close.
//
// The Locked postfix indicates that the caller must hold the versionLock.
func (c *snapshotEngine) brickLocked(err error) {
	if c.fatalErr == nil {
		c.fatalErr = err
	}
	c.cancel()
	c.lifecycleBackpressureCond.Broadcast()

	// Stop serving reads, on every shard rather than only one that may have failed: the engine has
	// failed, so no shard can vouch for its data. Cancelling the context alone would not do it —
	// only reads that block observe the context.
	//
	// Taking shard locks here is sound: versionLock is held and versionLock-before-shard-lock is the
	// established order (see Commit), and nothing acquires versionLock while holding a shard lock
	// (see the cache field on shard).
	for _, s := range c.shards {
		s.takeOutOfService(err)
	}
}

// Flushes and retires snapshots. Continues running until there is no more work, then returns.
func (c *snapshotEngine) doLifecycleWork() error {
	hasWork := true

	for hasWork {

		c.versionLock.Lock()

		firstFlushVersion, lastFlushVersion, versionWrites, err := c.determineVersionsToFlushLocked()
		if err != nil {
			c.versionLock.Unlock()
			return fmt.Errorf("unable to determine versions to flush: %w", err)
		}

		firstRetireVersion, lastRetireVersion := c.determineVersionsToRetireLocked()

		unflushedCount := lastFlushVersion - firstFlushVersion
		unretiredCount := lastRetireVersion - firstRetireVersion
		hasWork = unflushedCount > 0 || unretiredCount > 0

		c.versionLock.Unlock()

		err = c.flushSnapshots(firstFlushVersion, lastFlushVersion, versionWrites)
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
	// The finalization writes of the versions to be flushed.
	versionWrites map[uint64][]*proto.KVPair,
	err error,
) {

	firstVersion = c.oldestVersion
	lastVersion = c.oldestVersion
	versionWrites = make(map[uint64][]*proto.KVPair)

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

		if !counter.finalized {
			// Unfinalized snapshots are not flush eligible.
			break
		}

		// Mark the current snapshot as flush eligible.
		versionWrites[targetVersion] = counter.finalWrites
		lastVersion++

		if counter.referenceCount > 0 {
			// We've encountered a snapshot that is not retirement eligible. Although this snapshot
			// can be safely flushed, all later snapshots must wait on this non-retired snapshot.
			break
		}
	}

	return firstVersion, lastVersion, versionWrites, nil
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
	// The finalization writes of each version to flush.
	versionWrites map[uint64][]*proto.KVPair,
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
			// diffIndex is bounded by the version count, so this conversion is safe.
			version := firstVersion + uint64(diffIndex) //nolint:gosec
			for key, value := range diff {
				diffsByVersion[version][key] = value
			}
		}
	}

	// Fold each version's finalization writes into its diff, so that the caller's metadata is written
	// to the DB atomically with its block's data. A nil value in the diff map is a tombstone, so a
	// Delete pair maps to nil and a pair carrying an empty value is normalized to a non-nil empty
	// slice to keep the two distinguishable.
	for version := firstVersion; version < lastVersion; version++ {
		for _, pair := range versionWrites[version] {
			if pair.Delete {
				diffsByVersion[version][string(pair.Key)] = nil
				continue
			}
			value := pair.Value
			if value == nil {
				value = []byte{}
			}
			diffsByVersion[version][string(pair.Key)] = value
		}
	}

	// Write diffs to the DB in batches, oldest version first.
	var batch types.Batch
	defer func() {
		if batch != nil {
			_ = batch.Close()
		}
	}()
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

		// Len is non-negative, so the conversion is safe.
		if uint64(batch.Len()) >= c.config.TargetBytesPerFlush { //nolint:gosec
			commitErr := batch.Commit(types.WriteOptions{Sync: c.config.FlushSync})
			closeErr := batch.Close()
			batch = nil
			if commitErr != nil {
				return fmt.Errorf("flush failed to commit batch: %w", commitErr)
			}
			if closeErr != nil {
				return fmt.Errorf("flush failed to close batch: %w", closeErr)
			}
			if err := c.recordFlushedVersions(versionsInBatch); err != nil {
				return fmt.Errorf("flush failed to record flushed versions: %w", err)
			}
			versionsInBatch = 0
		}
	}
	if batch != nil {
		commitErr := batch.Commit(types.WriteOptions{Sync: c.config.FlushSync})
		closeErr := batch.Close()
		batch = nil
		if commitErr != nil {
			return fmt.Errorf("flush failed to commit batch: %w", commitErr)
		}
		if closeErr != nil {
			return fmt.Errorf("flush failed to close batch: %w", closeErr)
		}
		if err := c.recordFlushedVersions(versionsInBatch); err != nil {
			return fmt.Errorf("flush failed to record flushed versions: %w", err)
		}
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

// recordFlushedVersions debits versionCount from the unflushed version count and releases any Commit
// blocked on lifecycle backpressure. Returns an error if versionCount exceeds the number of versions
// currently believed to be unflushed, which means the flush and eligibility scans have disagreed
// about the flush set: an unsigned underflow here would wedge every future Commit in backpressure
// with no error, so the disagreement is reported instead.
func (c *snapshotEngine) recordFlushedVersions(versionCount uint64) error {
	c.versionLock.Lock()
	if versionCount > c.unflushedCount {
		c.versionLock.Unlock()
		return fmt.Errorf("flushed version count (%d) exceeds the unflushed version count (%d)",
			versionCount, c.unflushedCount)
	}
	c.unflushedCount -= versionCount
	c.versionLock.Unlock()

	c.lifecycleBackpressureCond.Signal()
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
			return fmt.Errorf("failed to drop versions from shard %d: %w", i, err)
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

// EscapeHatchUnderlyingDB returns the raw backing database, bypassing every guarantee this engine
// provides.
//
// The name is deliberately obstructive. Reading through it sees only what the flusher has written, so it
// misses both the rows the current version has staged and the rows finalized but not yet flushed —
// silently, with no error. Writing through it races the flusher, which will overwrite the same keys.
// Neither failure is detectable from the returned value.
//
// The only sanctioned use is an operation that must address the database as a file rather than as a
// key-value store, which in practice means taking a checkpoint. Every other use is a bug. If a caller
// wants to read data, it wants Get, BatchGet or Iterator; if it wants to write data, it wants Set,
// BatchSet or Finalize.
func (c *snapshotEngine) EscapeHatchUnderlyingDB() types.KeyValueDB {
	return c.db
}

func (c *snapshotEngine) Name() string {
	return c.config.Name
}

// Close is idempotent: teardown runs exactly once and subsequent calls return the same result.
func (c *snapshotEngine) Close() error {
	c.closeOnce.Do(func() {
		c.closeErr = c.closeInternal()
	})
	return c.closeErr
}

func (c *snapshotEngine) closeInternal() error {
	// Tell the lifecycle runner to exit, then wait for it to report offline. The send is
	// buffered, so it does not block when the runner has already exited (engine failure), and
	// the runner is guaranteed to close lifecycleExited (its defer runs even on panic).
	c.lifecycleExit <- struct{}{}
	<-c.lifecycleExited

	// Release everyone blocked on the engine's future: AwaitFlush, backpressured
	// Snapshot callers, and reads still awaiting results. The cancel happens under versionLock
	// because backpressure waiters re-check the context under that lock before parking on the
	// cond; a lockless cancel could slip between a waiter's check and its Wait, losing the
	// wakeup.
	c.versionLock.Lock()
	c.cancel()

	// Stop serving reads and accepting writes, the same as a brick does: the runner is gone, so any
	// write accepted from here on could never be flushed. First failure wins inside the shard, so a
	// brick that already ran keeps reporting its own cause rather than ErrEngineClosed.
	for _, s := range c.shards {
		s.takeOutOfService(ErrEngineClosed)
	}
	c.versionLock.Unlock()
	c.lifecycleBackpressureCond.Broadcast()

	// Wait for the metrics scrape loop (if any) to observe the cancellation and exit.
	c.metrics.awaitStopped()

	// The engine owns the database, so it closes it. This happens after the lifecycle runner has
	// reported offline, so no flush can still be in flight against it.
	dbErr := c.db.Close()

	c.versionLock.Lock()
	defer c.versionLock.Unlock()
	if c.fatalErr != nil {
		return fmt.Errorf("snapshot engine failed: %w", c.fatalErr)
	}
	if dbErr != nil {
		return fmt.Errorf("close underlying database: %w", dbErr)
	}
	return nil
}
