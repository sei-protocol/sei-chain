package view

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"sort"
	"sync"

	dbm "github.com/tendermint/tm-db"

	"github.com/sei-protocol/sei-chain/sei-db/common/threading"
	"github.com/sei-protocol/sei-chain/sei-db/common/utils"
	"github.com/sei-protocol/sei-chain/sei-db/db_engine/types"
	"github.com/sei-protocol/sei-chain/sei-db/proto"
	"github.com/sei-protocol/seilog"
)

var logger = seilog.NewLogger("db", "db-engine", "view")

var _ ViewManager = (*viewManager)(nil)

// The standard implementation of ViewManager.
type viewManager struct {
	// Manager-private context. Blocked waits throughout the manager select on it; it is cancelled
	// (always under versionLock — see closeInternal) when the manager shuts down, via Close or a
	// fatal lifecycle error.
	ctx    context.Context
	cancel context.CancelFunc

	config ViewManagerConfig

	// A utility for assigning keys to shard indices.
	shardManager *shardManager

	// The shards in the manager.
	shards []*shard

	// A pool for asynchronous reads.
	readPool threading.Pool

	// A pool for miscellaneous operations that are neither computationally intensive nor IO bound.
	miscPool threading.Pool

	// A pool for ordering each sealed version's writes by key, ahead of the flush that consumes them.
	sortPool threading.Pool

	// The underlying key-value database.
	db types.KeyValueDB

	// Each flush to the underlying database waits until this counter reaches the highest block height
	// the flush writes.
	commitCounter threading.ReadOnlyCounter

	// Protects modification to version state.
	versionLock *sync.Mutex

	// The current version number, which is the block height of the block being written. All modifications to
	// the manager will happen at this version number.
	//
	// Written only while holding versionLock (by Commit()). Read without the lock by the
	// Get/BatchGet paths; this is sound only because the manager contract forbids calling
	// Commit() concurrently with operations on the current version, so no lockless read can
	// race the write.
	currentBlockHeight uint64

	// The block height of the oldest view we are currently tracking.
	//
	// Protected by versionLock.
	oldestBlockHeight uint64

	// Reference counts for all views we are currently tracking. The current (mutable) version
	// does not have a reference count and so it does not appear in this map.
	//
	// Protected by versionLock.
	versionMap map[uint64]*viewReferenceCounter

	// The number of views that are eligible to be flushed to disk, but which have not yet been
	// flushed. Versions blocked behind a still-referenced view are not counted until that
	// view is released (see scanForFlushEligibilityLocked), so this measures work the
	// underlying DB can actually perform — Commit backpressure driven by it reflects DB lag, not
	// release lag.
	//
	// Protected by versionLock.
	unflushedCount uint64

	// One past the highest view version that either has been flushed or can be flushed. There may be older
	// view versions that have not yet been flushed, but it is guaranteed that any view version older than this
	// value is likewise eligible to be flushed or has already been flushed.
	//
	// Protected by versionLock.
	flushScanBlockHeight uint64

	// One past the highest view version that is eligible for retirement.
	//
	// Protected by versionLock.
	retireScanBlockHeight uint64

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

	// Metrics for recording view manager statistics.
	metrics *ViewManagerMetrics

	// The error that bricked the manager, latched by the lifecycle runner when lifecycle work
	// (e.g. a flush) fails. Once set it is never cleared: the lifecycle exits, the manager context
	// is cancelled, and methods that observe the shutdown report this error. Nil if the manager
	// has not failed.
	//
	// Protected by versionLock.
	fatalErr error

	// Ensures Close runs its teardown exactly once; closeErr memoizes the result so repeat calls
	// return the same error without re-running teardown.
	closeOnce sync.Once
	closeErr  error
}

// Tracks the reference count for a particular view.
type viewReferenceCounter struct {
	// The block height of the view.
	blockHeight uint64

	// The number of reservations currently held for this view. When this count reaches 0,
	// the view is eligible for retirement.
	referenceCount uint64

	// True once FinalizeView has run for this view. Flushing is gated on it. By contract the
	// finalizing consumer holds a reservation until it has finalized, so this is guaranteed to be
	// true before referenceCount reaches 0. Enforced in DecrementReferenceCount.
	finalized bool

	// The metadata pairs supplied to FinalizeView, written to disk in the same atomic batch as
	// this view's diff. May be empty: a consumer with nothing to record still finalizes.
	finalWrites []*proto.KVPair

	// True if the view has been flushed, otherwise false.
	flushedToDisk bool

	// Closed by flushViews when this view's data has been persisted to disk. Pairs with
	// flushedToDisk. Used as a synchronization handle for AwaitFlush waiters; a closed channel is
	// immediately selectable, so the "already flushed at call time" case requires no special path.
	flushCompleted chan struct{}
}

// Creates a new ViewManager.
//
// The manager takes ownership of db and closes it in Close. Nothing else may read or write that database
// afterwards: doing so bypasses the manager's staging and cache and sees or corrupts a version nobody
// asked for. The pools, by contrast, are shared and remain the caller's to close — after the manager,
// since the manager's goroutines submit to them.
func NewViewManager(
	config *ViewManagerConfig,
	// The block height the underlying database holds. The first version the manager seals is this height.
	initialHeight uint64,
	// Each flush to db waits until this counter reaches the highest block height it writes. A flush that finds
	// the counter sealed below that height is abandoned, and nothing further is flushed.
	commitCounter threading.ReadOnlyCounter,
	// The underlying key-value database.
	db types.KeyValueDB,
	// A work pool for reading from the DB.
	readPool threading.Pool,
	// A work pool for miscellaneous operations that are neither computationally intensive nor IO bound.
	// Must not be the same bounded pool as readPool: tasks submitted here block waiting on
	// readPool results, so sharing one fixed-size pool can deadlock under load. Pass distinct
	// pools, or an elastic pool.
	miscPool threading.Pool,
	// A work pool for ordering each sealed version's writes by key.
	//
	// Its queue must be large enough never to fill in practice, because Commit submits to it,
	// and backpressure on commits belongs to MaxUnflushedVersions alone. The count of outstanding jobs
	// is not bounded by that setting either, because a version held by an outside reservation stops
	// being counted as unflushed (see scanForFlushEligibilityLocked) while its successors keep being
	// sealed.
	sortPool threading.Pool,
) (ViewManager, error) {
	if err := config.Validate(); err != nil {
		return nil, fmt.Errorf("invalid view manager config: %w", err)
	}
	if commitCounter == nil {
		return nil, fmt.Errorf("commitCounter must be non-nil")
	}

	shardManager, err := newShardManager(config.ShardCount)
	if err != nil {
		return nil, fmt.Errorf("failed to create shard manager: %w", err)
	}
	sizePerShard := config.MaxSize / config.ShardCount

	// Everything owned by the manager (shards, the metrics scrape loop, blocked waits) lives on
	// this manager-private context, cancelled only when the manager shuts down.
	childCtx, cancel := context.WithCancel(context.Background())

	versionLock := &sync.Mutex{}
	lifecycleBackpressureCond := sync.NewCond(versionLock)

	c := &viewManager{
		ctx:                       childCtx,
		cancel:                    cancel,
		config:                    *config,
		shardManager:              shardManager,
		readPool:                  readPool,
		miscPool:                  miscPool,
		sortPool:                  sortPool,
		db:                        db,
		commitCounter:             commitCounter,
		versionMap:                make(map[uint64]*viewReferenceCounter),
		currentBlockHeight:        initialHeight,
		oldestBlockHeight:         initialHeight,
		flushScanBlockHeight:      initialHeight,
		retireScanBlockHeight:     initialHeight,
		versionLock:               versionLock,
		lifecycleBackpressureCond: lifecycleBackpressureCond,
		lifecycleWake:             make(chan struct{}, 1),
		lifecycleExit:             make(chan struct{}, 1),
		lifecycleExited:           make(chan struct{}),
	}

	// Shards are created after the manager struct so they can report the manager's shutdown error
	// (the latched fatal error, or ErrViewManagerClosed) when a blocked read is released by context
	// cancellation — see the Close contract on ViewManager.
	shards := make([]*shard, config.ShardCount)
	for i := uint64(0); i < config.ShardCount; i++ {
		shards[i], err = NewShard(childCtx, config, db, readPool, sizePerShard, initialHeight,
			c.shutdownError, c.reportReadFailure)
		if err != nil {
			cancel()
			return nil, fmt.Errorf("failed to create shard: %w", err)
		}
	}
	c.shards = shards

	if config.MetricsEnabled {
		metrics := newViewManagerMetrics(
			childCtx, config.Name, config.MetricsScrapeInterval(), c.getCacheSizeInfo, config.ShardCount)
		for i, s := range c.shards {
			s.metrics = metrics.shard(i)
			s.cache.metrics = s.metrics
		}
		c.metrics = metrics
	}

	go c.lifecycleRunner()

	return c, nil
}

func (c *viewManager) getCacheSizeInfo() (bytes uint64, entries uint64) {
	for _, s := range c.shards {
		b, e := s.GetSizeInfo()
		bytes += b
		entries += e
	}
	return bytes, entries
}

func (c *viewManager) BatchSet(writes []Write) error {
	buckets := c.bucketIndicesByShard(len(writes), func(i int) string { return writes[i].Key })

	// Fan out to shards. A shard refusing the write — it is out of service, so the manager is closed or
	// bricked — fails the whole call; the shards that accepted it have already applied their entries, so
	// the batch is not atomic across shards in that case. That is acceptable because the manager contract
	// makes any error fatal.
	var wg sync.WaitGroup
	errs := make([]error, len(buckets))
	for shardIndex := range buckets {
		if len(buckets[shardIndex]) == 0 {
			continue
		}
		wg.Add(1)
		c.miscPool.Submit(func() {
			defer wg.Done()
			errs[shardIndex] = c.shards[shardIndex].batchSetAt(writes, buckets[shardIndex])
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

// bucketIndicesByShard decides which shard each key of a batch belongs to.
//
// byShard is indexed by shard, and each entry lists the batch indices of that shard's keys:
// byShard[X][Y] == Z means key Y of shard X is key Z of the batch, and len(byShard[X]) is the number
// of keys belonging to shard X. Z increases with Y, so a shard's keys stay in batch order.
func (c *viewManager) bucketIndicesByShard(
	// The number of keys in the batch.
	count int,
	// Supplies the key at an index into the batch.
	keyAt func(int) string,
) (byShard [][]int) {
	byShard = make([][]int, len(c.shards))

	chunks := min(len(c.shards), count)
	if chunks <= 1 {
		for i := 0; i < count; i++ {
			shardIndex := c.shardManager.ShardString(keyAt(i))
			byShard[shardIndex] = append(byShard[shardIndex], i)
		}
		return byShard
	}

	// Every key must be placed before any shard can be locked, so this is a batch's longest serial
	// stretch. Each chunk fills its own buckets, so no two writers touch the same slice.
	chunkSize := (count + chunks - 1) / chunks
	partial := make([][][]int, chunks)
	var wg sync.WaitGroup
	for chunk := range partial {
		start := chunk * chunkSize
		end := min(start+chunkSize, count)
		wg.Add(1)
		c.miscPool.Submit(func() {
			defer wg.Done()
			local := make([][]int, len(c.shards))
			for i := start; i < end; i++ {
				shardIndex := c.shardManager.ShardString(keyAt(i))
				local[shardIndex] = append(local[shardIndex], i)
			}
			partial[chunk] = local
		})
	}
	wg.Wait()

	// Concatenated in chunk order, which is batch order, since a chunk is a contiguous range.
	for shardIndex := range byShard {
		total := 0
		for chunk := range partial {
			total += len(partial[chunk][shardIndex])
		}
		if total == 0 {
			continue
		}
		byShard[shardIndex] = make([]int, 0, total)
		for chunk := range partial {
			byShard[shardIndex] = append(byShard[shardIndex], partial[chunk][shardIndex]...)
		}
	}
	return byShard
}

func (c *viewManager) BatchGet(keys [][]byte) (map[string][]byte, error) {
	return c.BatchGetAtBlockHeight(keys, c.currentBlockHeight)
}

// Similar semantics to BatchGet, but reads at the given block height.
func (c *viewManager) BatchGetAtBlockHeight(keys [][]byte, blockHeight uint64) (map[string][]byte, error) {
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
			results[i], errs[i] = c.shards[shardIndex].BatchGet(work[shardIndex], blockHeight)
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

func (c *viewManager) Delete(key []byte) error {
	shardIndex := c.shardManager.Shard(key)
	shard := c.shards[shardIndex]
	if err := shard.Delete(key); err != nil {
		return fmt.Errorf("failed to delete key in shard: %w", err)
	}
	return nil
}

func (c *viewManager) Get(key []byte, updateLru bool) ([]byte, bool, error) {
	return c.GetAtBlockHeight(key, c.currentBlockHeight, updateLru)
}

func (c *viewManager) GetAtBlockHeight(key []byte, blockHeight uint64, updateLru bool) ([]byte, bool, error) {
	shardIndex := c.shardManager.Shard(key)
	shard := c.shards[shardIndex]

	value, ok, err := shard.Get(key, blockHeight, updateLru)
	if err != nil {
		return nil, false, fmt.Errorf("failed to get value from shard: %w", err)
	}
	if !ok {
		return nil, false, nil
	}
	return value, ok, nil
}

func (c *viewManager) Set(key []byte, value []byte) error {
	shardIndex := c.shardManager.Shard(key)
	shard := c.shards[shardIndex]
	if err := shard.Set(key, value); err != nil {
		return fmt.Errorf("failed to set key in shard: %w", err)
	}
	return nil
}

func (c *viewManager) Commit() (View, error) {
	// Reset the phase on every exit so error returns don't leave the timer stuck on a phase.
	defer c.metrics.setViewPhase("")

	c.metrics.setViewPhase("acquire_version_lock")
	c.versionLock.Lock()

	// A bricked manager does no more work. Free to check here: versionLock, which guards fatalErr,
	// is already held.
	if c.fatalErr != nil {
		err := fmt.Errorf("cannot create view: %w", c.shutdownErrorLocked())
		c.versionLock.Unlock()
		return nil, err
	}

	c.metrics.setViewPhase("lifecycle_backpressure")

	if err := c.lifecycleBackpressureLocked(); err != nil {
		c.versionLock.Unlock()
		return nil, fmt.Errorf("cannot create view: %w", err)
	}

	sealedBlockHeight := c.currentBlockHeight

	c.versionMap[sealedBlockHeight] = &viewReferenceCounter{
		blockHeight:    sealedBlockHeight,
		referenceCount: 1,
		flushCompleted: make(chan struct{}),
	}

	view := &viewImpl{
		blockHeight:   sealedBlockHeight,
		parentManager: c,
	}

	c.currentBlockHeight++

	c.metrics.setViewPhase("shards_view")

	if err := c.sealShardsLocked(); err != nil {
		// Any shard failing leaves the shards disagreeing about the current version, so the manager
		// is bricked rather than left callable.
		c.brickLocked(err)
		c.versionLock.Unlock()
		return nil, err
	}

	// Unlocked explicitly rather than deferred, because the sort submitted below must not hold the
	// versionLock: it can block when the sort pool's queue is full, and the queue drains only as the
	// flush consumes results — which needs the versionLock. Blocking there while holding it would
	// deadlock against the very work that would unblock it.
	c.versionLock.Unlock()

	c.metrics.setViewPhase("submit_diff_sort")
	c.materializeDiffAtBlockHeight(sealedBlockHeight)

	view.closed = utils.MustClose(view, "view")
	return view, nil
}

// sealShardsLocked seals on every shard the version the manager has just moved past, reporting what
// went wrong on any of them.
//
// The Locked postfix indicates that the caller must hold the versionLock.
func (c *viewManager) sealShardsLocked() error {
	// Read once here rather than from the tasks, which do not hold the versionLock that guards it.
	expectedBlockHeight := c.currentBlockHeight

	errs := make([]error, len(c.shards))
	var wg sync.WaitGroup
	wg.Add(len(c.shards))
	// One task per shard. Sealing a shard is almost entirely the wait to take its lock from the
	// readers holding it, and those waits are independent, so overlapping them costs one
	// wait rather than eight. Holding every shard lock at once is safe because no shard lock holder
	// reaches back for the versionLock (see the cache field on shard).
	for i, shard := range c.shards {
		c.miscPool.Submit(func() {
			defer wg.Done()
			shardBlockHeight, err := shard.Commit()
			if err != nil {
				// Either the shard was out of service and sealed nothing, or it sealed and then could
				// not maintain its read cache.
				errs[i] = fmt.Errorf("seal shard %d: %w", i, err)
				return
			}
			if shardBlockHeight != expectedBlockHeight {
				// Should be impossible: the manager is now inconsistent, with some shards sealed and
				// some not.
				errs[i] = fmt.Errorf("shard %d is at version %d, but the manager is at %d",
					i, shardBlockHeight, expectedBlockHeight)
			}
		})
	}
	// Awaited even once a failure is known: a shard still sealing would otherwise write its result
	// after the caller had bricked the manager.
	wg.Wait()

	return errors.Join(errs...)
}

// This method blocks if the lifecycle runner is not keeping up. It is assumed that the caller already holds the
// versionLock. When this method returns, it will still hold the versionLock, but it may release and then
// re-acquire versionLock internally as it awaits for the lifecycle runner to catch up.
func (c *viewManager) lifecycleBackpressureLocked() error {
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

// shutdownErrorLocked builds the error reported by methods that observe manager shutdown: the
// latched fatal error when the lifecycle runner failed, otherwise ErrViewManagerClosed.
//
// The Locked postfix indicates that the caller must hold the versionLock.
func (c *viewManager) shutdownErrorLocked() error {
	if c.fatalErr != nil {
		return fmt.Errorf("view manager failed: %w", c.fatalErr)
	}
	return ErrViewManagerClosed
}

// shutdownError is the unlocked variant of shutdownErrorLocked. The caller must NOT hold versionLock.
func (c *viewManager) shutdownError() error {
	c.versionLock.Lock()
	defer c.versionLock.Unlock()
	return c.shutdownErrorLocked()
}

// Increment the reference count for the view at the given block height.
func (c *viewManager) IncrementReferenceCount(blockHeight uint64) error {
	c.versionLock.Lock()
	defer c.versionLock.Unlock()

	if blockHeight < c.oldestBlockHeight {
		return fmt.Errorf("version (%d) is less than the oldest version (%d)", blockHeight, c.oldestBlockHeight)
	}
	if blockHeight >= c.currentBlockHeight {
		return fmt.Errorf("version (%d) must be less than the current version (%d)",
			blockHeight, c.currentBlockHeight)
	}

	counter, ok := c.versionMap[blockHeight]
	if !ok {
		// Should be impossible since version retirement never leaves gaps
		return fmt.Errorf("version (%d) not found", blockHeight)
	}

	if counter.referenceCount == 0 {
		return fmt.Errorf("version (%d) has already been dropped", blockHeight)
	}

	counter.referenceCount++
	return nil
}

// DecrementReferenceCount releases one reservation on the view at blockHeight, reporting whether it was the last one.
func (c *viewManager) DecrementReferenceCount(blockHeight uint64) (lastReleased bool, err error) {
	c.versionLock.Lock()
	defer c.versionLock.Unlock()

	if blockHeight < c.oldestBlockHeight {
		return false, fmt.Errorf("version (%d) is less than the oldest version (%d)",
			blockHeight, c.oldestBlockHeight)
	}
	if blockHeight >= c.currentBlockHeight {
		return false, fmt.Errorf("version (%d) must be less than the current version (%d)",
			blockHeight, c.currentBlockHeight)
	}

	counter, ok := c.versionMap[blockHeight]
	if !ok {
		// Should be impossible since version retirement never leaves gaps
		return false, fmt.Errorf("version (%d) not found", blockHeight)
	}

	if counter.referenceCount == 0 {
		return false, fmt.Errorf("version (%d) has already been dropped", blockHeight)
	}

	if counter.referenceCount == 1 && !counter.finalized {
		// Releasing the last reservation without finalizing is fatal (see the finalization-duty
		// contract on View). This view can now never make progress:
		// determineBlocksToFlushLocked skips unfinalized versions, retirement requires a flush, and
		// finalization will never arrive because the caller has spent its Release. Every later version
		// stalls behind it with its in-memory data accumulating. Note the reference count is not what
		// traps it — decrementing would not help.
		//
		// The sibling errors in this method deliberately do not brick: those reject a bogus version
		// and leave manager state untouched, so that caller can retry with the right one.
		err := fmt.Errorf("version (%d) was fully released without first being finalized", blockHeight)
		c.brickLocked(err)
		return false, err
	}

	counter.referenceCount--

	c.maybeWakeLifecycleLocked()

	return counter.referenceCount == 0, nil
}

// Scans for new flush eligible versions. Returns true if there is a new flush eligible version discovered.
//
// The Locked postfix indicates that the caller must hold the versionLock.
func (c *viewManager) scanForFlushEligibilityLocked() bool {
	oldFlushScanBlockHeight := c.flushScanBlockHeight

	if oldFlushScanBlockHeight > c.oldestBlockHeight {
		if counter, ok := c.versionMap[oldFlushScanBlockHeight-1]; ok && counter.referenceCount > 0 {
			// The version at the watermark is still referenced, so nothing past it can be flushed
			// until it is released and retired.
			return false
		}
	}

	// Clip the start to oldestBlockHeight: prior retirements may have advanced oldestBlockHeight past the
	// stale oldFlushScanBlockHeight, leaving any versions in between absent from versionMap.
	start := max(oldFlushScanBlockHeight, c.oldestBlockHeight)

	for blockHeight := start; blockHeight < c.currentBlockHeight; blockHeight++ {
		counter := c.versionMap[blockHeight]

		if !counter.finalized {
			// We can only flush finalized views. If we hit an unfinalized one, no more flushing is
			// possible.
			break
		}

		if !counter.flushedToDisk {
			// This view is flush eligible.
			c.unflushedCount++
			c.flushScanBlockHeight = blockHeight + 1
		}

		if counter.referenceCount > 0 {
			// The first view that is not fully released is flush eligible, but all following
			// views are not.
			break
		}
	}

	return c.flushScanBlockHeight > oldFlushScanBlockHeight
}

// Scans for new retirement eligible versions. Returns true if there is a new retirement eligible version discovered.
//
// The Locked postfix indicates that the caller must hold the versionLock.
func (c *viewManager) scanForRetirementEligibilityLocked() bool {
	oldRetireScanBlockHeight := c.retireScanBlockHeight

	// Clip the start to oldestBlockHeight: prior retirements may have advanced oldestBlockHeight past the
	// stale oldRetireScanBlockHeight, leaving any versions in between absent from versionMap.
	start := max(oldRetireScanBlockHeight, c.oldestBlockHeight)

	for blockHeight := start; blockHeight < c.currentBlockHeight; blockHeight++ {
		counter := c.versionMap[blockHeight]
		if counter.referenceCount != 0 || !counter.flushedToDisk {
			// We can only retire views that are fully released and flushed to disk.
			break
		}
		c.retireScanBlockHeight = blockHeight + 1
	}

	return c.retireScanBlockHeight > oldRetireScanBlockHeight
}

// Determine if we need to wake up the lifecycle runner to do work (either flushing or retiring views).
//
// The Locked postfix indicates that the caller must hold the versionLock.
func (c *viewManager) maybeWakeLifecycleLocked() {
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

// FinalizeView attaches metadata writes to the view at the given block height and makes it
// eligible to be flushed. An empty write set is legal.
func (c *viewManager) FinalizeView(blockHeight uint64, writes []*proto.KVPair) error {
	c.versionLock.Lock()
	defer c.versionLock.Unlock()

	counter, ok := c.versionMap[blockHeight]
	if !ok {
		return fmt.Errorf("version (%d) not found", blockHeight)
	}

	if counter.finalized {
		return fmt.Errorf("version %d has already been finalized", blockHeight)
	}

	counter.finalized = true
	counter.finalWrites = writes

	c.maybeWakeLifecycleLocked()

	return nil
}

// ForEachDiffAtBlockHeight visits every write at a sealed version, waiting for it to be materialized if that
// has not happened yet. Shard by shard, so the keys arrive ordered within a shard but not across them: no
// consumer of a whole version's diff depends on a global order, and merging the shards to provide one
// would cost the caller a comparison per key for nothing.
func (c *viewManager) ForEachDiffAtBlockHeight(blockHeight uint64, visit func(key string, value []byte) error) error {
	for i, shard := range c.shards {
		diff, err := shard.SortedDiff(blockHeight)
		if err != nil {
			return fmt.Errorf("failed to get the diff of shard %d at version %d: %w", i, blockHeight, err)
		}
		for _, entry := range diff {
			if err := visit(entry.Key, entry.Value); err != nil {
				return err
			}
		}
	}
	return nil
}

func (c *viewManager) Iterator(opts *types.IterOptions) (dbm.Iterator, error) {
	// Overrides first, DB iterator second, and the order is load-bearing: a concurrent flush+retire
	// that moved data out of versionedData and into the DB between the two steps would drop those
	// keys entirely if the DB view were taken first. In this order the same race can only yield a
	// key twice, which the merge resolves in favor of the override.
	overrides, err := c.MaterializeCurrentOverrides(opts)
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
	iter, err := newViewIterator(
		overrides, dbIter, []byte(c.config.ReservedPrefix), reverse, lowerBound, upperBound)
	if err != nil {
		return nil, fmt.Errorf("failed to create view iterator: %w", err)
	}

	// Register the iterator only now that construction has fully succeeded, so a failed construction
	// cannot leave a phantom entry behind.
	for _, s := range c.shards {
		s.IteratorOpened()
	}
	tracked := &trackedIterator{Iterator: iter, manager: c}
	tracked.closed = utils.MustClose(tracked, "view manager iterator")
	return tracked, nil
}

// trackedIterator deregisters itself from every shard when closed, so Close can report the iterators
// still outstanding. Close is idempotent, and deregisters exactly once however often it is called.
type trackedIterator struct {
	dbm.Iterator
	manager   *viewManager
	closeOnce sync.Once

	// closed records whether Close has been called.
	closed utils.CloseMarker[trackedIterator]
}

func (w *trackedIterator) Close() error {
	var err error
	w.closeOnce.Do(func() {
		w.closed.Close(w)
		errs := make([]error, 0, len(w.manager.shards)+1)
		for _, s := range w.manager.shards {
			errs = append(errs, s.IteratorClosed())
		}
		errs = append(errs, w.Iterator.Close())
		err = errors.Join(errs...)
	})
	return err
}

// MaterializeCurrentOverrides gathers the in-memory overrides at the current version from every
// shard and returns them sorted ascending by key. Each shard is responsible for its own locking;
// here we just stitch the results together, and the sort runs without any shard lock held.
// The overrides are sorted into iteration order — ascending, or descending when reverse is set — so
// the merge in viewIterator can walk them and the DB iterator in lockstep.
func (c *viewManager) MaterializeCurrentOverrides(opts *types.IterOptions) ([]kvPair, error) {
	var lowerBound, upperBound []byte
	reverse := false
	if opts != nil {
		lowerBound, upperBound, reverse = opts.LowerBound, opts.UpperBound, opts.Reverse
	}

	var all []kvPair
	for i, s := range c.shards {
		shardOverrides, err := s.MaterializeCurrentOverrides(lowerBound, upperBound)
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

// Retire old versions: flush their data to the underlying DB, then free the in-memory views.
// The runner sleeps until signaled via lifecycleWake (see wakeLifecycle / scanRetirementEligibility),
// then processes all available work before sleeping again. It exits only via its lifecycleExit
// inbox (sent by Close) or by bricking on a fatal error.
func (c *viewManager) lifecycleRunner() {
	defer close(c.lifecycleExited)

	for {
		select {
		case <-c.lifecycleExit:
			return
		case <-c.ctx.Done():
			// The manager was bricked from outside the runner (see Commit): the version state is
			// no longer trustworthy, so stop doing lifecycle work. Close cancels the context only
			// after this goroutine has exited, so this case never fires on a clean shutdown.
			return
		case <-c.lifecycleWake:
		}

		stopped, err := c.doLifecycleWork()
		if err != nil {
			// Brick the manager and exit. The caller is expected to halt the node on the
			// first error it sees.
			c.brick(err)
			return
		}
		if stopped {
			return
		}
	}
}

// brick latches the fatal error and releases everyone blocked on the manager, so callers observe
// the failure immediately rather than waiting for Close.
func (c *viewManager) brick(err error) {
	c.versionLock.Lock()
	c.brickLocked(err)
	c.versionLock.Unlock()
}

// reportReadFailure handles a failed DB read by bricking the manager.
//
// Halting is the caller's responsibility, not ours (see the ViewManager doc). This exists so that
// a caller which ignores the failure gets nowhere, not to police it: reads racing this call may still
// succeed, and no promise is made about when they stop.
//
// Must be called without the shard lock held: it acquires versionLock, and the established order is
// versionLock before any shard lock.
func (c *viewManager) reportReadFailure(err error) {
	c.brick(fmt.Errorf("failed to read from the underlying database: %w", err))
}

// brickLocked latches the fatal error, cancels the manager context, wakes backpressure waiters, and
// takes every shard out of service, so callers observe the failure immediately rather than waiting
// for Close.
//
// The Locked postfix indicates that the caller must hold the versionLock.
func (c *viewManager) brickLocked(err error) {
	if c.fatalErr == nil {
		c.fatalErr = err
	}
	c.cancel()
	c.lifecycleBackpressureCond.Broadcast()

	// Stop serving reads, on every shard rather than only one that may have failed: the manager has
	// failed, so no shard can vouch for its data. Cancelling the context alone would not do it —
	// only reads that block observe the context.
	//
	// Taking shard locks here is sound: versionLock is held and versionLock-before-shard-lock is the
	// established order (see Commit), and nothing acquires versionLock while holding a shard lock
	// (see the cache field on shard).
	for _, s := range c.shards {
		s.TakeOutOfService(err)
	}
}

// Flushes and retires views. Continues running until there is no more work, then returns. Reports stopped when
// the commit counter was sealed below a block height due to be flushed, in which case nothing more may be flushed.
func (c *viewManager) doLifecycleWork() (bool, error) {
	hasWork := true

	for hasWork {

		c.versionLock.Lock()

		firstFlushBlockHeight, lastFlushBlockHeight, finalWritesByBlockHeight, err :=
			c.determineBlocksToFlushLocked()
		if err != nil {
			c.versionLock.Unlock()
			return false, fmt.Errorf("unable to determine versions to flush: %w", err)
		}

		firstRetireBlockHeight, lastRetireBlockHeight := c.determineVersionsToRetireLocked()

		unflushedCount := lastFlushBlockHeight - firstFlushBlockHeight
		unretiredCount := lastRetireBlockHeight - firstRetireBlockHeight
		hasWork = unflushedCount > 0 || unretiredCount > 0

		c.versionLock.Unlock()

		stopped, err := c.flushViews(firstFlushBlockHeight, lastFlushBlockHeight, finalWritesByBlockHeight)
		if err != nil {
			return false, fmt.Errorf("unable to flush views: %w", err)
		}
		if stopped {
			return true, nil
		}

		err = c.retireViews(firstRetireBlockHeight, lastRetireBlockHeight)
		if err != nil {
			return false, fmt.Errorf("unable to retire views: %w", err)
		}
	}

	return false, nil
}

// Determine which versions need to be flushed to disk.
func (c *viewManager) determineBlocksToFlushLocked() (
	// The first version to be flushed, inclusive.
	firstBlockHeight uint64,
	// The last version to be flushed, exclusive.
	lastBlockHeight uint64,
	// The finalization writes of the versions to be flushed.
	finalWritesByBlockHeight map[uint64][]*proto.KVPair,
	err error,
) {

	firstBlockHeight = c.oldestBlockHeight
	lastBlockHeight = c.oldestBlockHeight
	finalWritesByBlockHeight = make(map[uint64][]*proto.KVPair)

	if c.oldestBlockHeight == c.currentBlockHeight {
		// The only version we are tracking is the mutable version, which is never flush eligible.
		return
	}

	// If the oldest tracked version has already been flushed, then there is a pending retirement
	// that hasn't run yet.
	if c.versionMap[c.oldestBlockHeight].flushedToDisk {
		return
	}

	for targetBlockHeight := firstBlockHeight; targetBlockHeight < c.currentBlockHeight; targetBlockHeight++ {
		counter := c.versionMap[targetBlockHeight]

		if !counter.finalized {
			// Unfinalized views are not flush eligible.
			break
		}

		// Mark the current view as flush eligible.
		finalWritesByBlockHeight[targetBlockHeight] = counter.finalWrites
		lastBlockHeight++

		if counter.referenceCount > 0 {
			// We've encountered a view that is not retirement eligible. Although this view
			// can be safely flushed, all later views must wait on this non-retired view.
			break
		}
	}

	return firstBlockHeight, lastBlockHeight, finalWritesByBlockHeight, nil
}

// Determine which versions need to be retired.
func (c *viewManager) determineVersionsToRetireLocked() (
	// The first version to be retired, inclusive.
	firstBlockHeight uint64,
	// The last version to be retired, exclusive.
	lastBlockHeight uint64,
) {

	firstBlockHeight = c.oldestBlockHeight
	lastBlockHeight = c.oldestBlockHeight

	for targetBlockHeight := c.oldestBlockHeight; targetBlockHeight < c.currentBlockHeight; targetBlockHeight++ {
		counter := c.versionMap[targetBlockHeight]

		if counter.referenceCount > 0 || !counter.flushedToDisk {
			// We can only retire versions that are entirely released and flushed to disk.
			break
		}

		// Include this view in the set to be retired
		lastBlockHeight++
	}

	return firstBlockHeight, lastBlockHeight
}

// flushViews writes the versions in [firstBlockHeight, lastBlockHeight) to the underlying DB in batches, each
// version's own writes ordered by key and followed by its finalization writes, and marks them flushed. Each batch
// is committed only once the commit counter reaches the batch's highest block height. Reports stopped, having marked
// nothing flushed, if the counter is sealed below a batch's highest block height.
func (c *viewManager) flushViews(
	// The first version to flush (inclusive).
	firstBlockHeight uint64,
	// The last version to flush (exclusive).
	lastBlockHeight uint64,
	// The finalization writes of each version to flush.
	finalWritesByBlockHeight map[uint64][]*proto.KVPair,
) (bool, error) {

	// Write diffs to the DB in batches, oldest version first.
	var batch types.Batch
	defer func() {
		if batch != nil {
			_ = batch.Close()
		}
	}()
	versionsInBatch := uint64(0)
	for blockHeight := firstBlockHeight; blockHeight < lastBlockHeight; blockHeight++ {
		versionsInBatch++
		if batch == nil {
			// Never sized up front: pebble hands back a pooled batch still carrying the buffer its last
			// use grew, and asking for a capacity replaces that buffer with a fresh allocation instead.
			batch = c.db.NewBatch()
		}

		// Ordered by key on the sort pool when the version was sealed, so this is a merge of finished
		// runs rather than a sort. One version at a time and never merged across versions: pebble
		// resolves two writes to one key by sequence number, which it assigns in batch order, so a key
		// written in several of a batch's versions must reach it oldest first.
		shardDiffs, err := c.materializeSortedDiffs(blockHeight)
		if err != nil {
			return false, err
		}
		err = forEachMergedEntry(shardDiffs, func(entry Write) error {
			if entry.Value == nil {
				return batch.DeleteString(entry.Key)
			}
			return batch.SetString(entry.Key, entry.Value)
		})
		if err != nil {
			return false, fmt.Errorf("flush failed to write the diff at version %d: %w", blockHeight, err)
		}

		// The caller's metadata goes in the same batch as its block's data, so the two land atomically,
		// and last, so that a metadata key colliding with a data key still wins. It cannot be part of the
		// merge above: that runs over diffs ordered when the version was sealed, and FinalizeView
		// supplies these writes later. A Delete pair becomes a tombstone, and a pair carrying an empty
		// value is normalized to a non-nil empty slice to keep the two distinguishable.
		for _, pair := range finalWritesByBlockHeight[blockHeight] {
			if pair.Delete {
				if err := batch.Delete(pair.Key); err != nil {
					return false, fmt.Errorf("flush failed to delete metadata key: %w", err)
				}
				continue
			}
			value := pair.Value
			if value == nil {
				value = []byte{}
			}
			if err := batch.Set(pair.Key, value); err != nil {
				return false, fmt.Errorf("flush failed to set metadata key: %w", err)
			}
		}

		// Len is non-negative, so the conversion is safe.
		if uint64(batch.Len()) >= c.config.TargetBytesPerFlush { //nolint:gosec
			stopped, err := c.commitFlushBatch(batch, blockHeight, versionsInBatch)
			batch = nil
			if err != nil || stopped {
				return stopped, err
			}
			versionsInBatch = 0
		}
	}
	if batch != nil {
		stopped, err := c.commitFlushBatch(batch, lastBlockHeight-1, versionsInBatch)
		batch = nil
		if err != nil || stopped {
			return stopped, err
		}
	}

	// Mark the flushed versions as having been flushed.
	c.versionLock.Lock()
	for blockHeight := firstBlockHeight; blockHeight < lastBlockHeight; blockHeight++ {
		counter := c.versionMap[blockHeight]
		counter.flushedToDisk = true
		close(counter.flushCompleted)
	}
	c.versionLock.Unlock()

	return false, nil
}

// commitFlushBatch waits for the commit counter to reach highestBlockHeight, then commits and closes the batch and
// records its versions as flushed. The batch is closed whatever the outcome. Reports stopped, without committing,
// if the counter is sealed below highestBlockHeight.
func (c *viewManager) commitFlushBatch(
	// The batch to commit.
	batch types.Batch,
	// The highest block height the batch writes.
	highestBlockHeight uint64,
	// The number of versions the batch writes.
	versionCount uint64,
) (bool, error) {
	reached, counterValue, err := c.commitCounter.Await(c.ctx, highestBlockHeight)
	if err != nil {
		_ = batch.Close()
		return false, fmt.Errorf("flush failed to await the commit counter at block height %d: %w",
			highestBlockHeight, err)
	}
	if !reached {
		_ = batch.Close()
		logger.Warn("abandoning flush: the commit counter was sealed below the block height being flushed, "+
			"and no further versions will be flushed",
			"manager", c.config.Name,
			"block_height", highestBlockHeight,
			"commit_counter", counterValue)
		return true, nil
	}

	commitErr := batch.Commit(types.WriteOptions{Sync: c.config.FlushSync})
	closeErr := batch.Close()
	if commitErr != nil {
		return false, fmt.Errorf("flush failed to commit batch: %w", commitErr)
	}
	if closeErr != nil {
		return false, fmt.Errorf("flush failed to close batch: %w", closeErr)
	}
	if err := c.recordFlushedVersions(versionCount); err != nil {
		return false, fmt.Errorf("flush failed to record flushed versions: %w", err)
	}
	return false, nil
}

// recordFlushedVersions debits versionCount from the unflushed version count and releases any Commit
// blocked on lifecycle backpressure. Returns an error if versionCount exceeds the number of versions
// currently believed to be unflushed, which means the flush and eligibility scans have disagreed
// about the flush set: an unsigned underflow here would wedge every future Commit in backpressure
// with no error, so the disagreement is reported instead.
func (c *viewManager) recordFlushedVersions(versionCount uint64) error {
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

// Retire all eligible views.
func (c *viewManager) retireViews(
	// The first version to retire (inclusive).
	firstBlockHeight uint64,
	// The last version to retire (exclusive).
	lastBlockHeight uint64,
) error {

	if firstBlockHeight >= lastBlockHeight {
		// Nothing to retire. Callers in doLifecycleWork pass the same first/last when
		// no version is retirement-eligible.
		return nil
	}

	for i, shard := range c.shards {
		err := shard.DropVersions(firstBlockHeight, lastBlockHeight)
		if err != nil {
			return fmt.Errorf("failed to drop versions from shard %d: %w", i, err)
		}
	}

	c.versionLock.Lock()
	defer c.versionLock.Unlock()

	for targetBlockHeight := firstBlockHeight; targetBlockHeight < lastBlockHeight; targetBlockHeight++ {
		counter := c.versionMap[targetBlockHeight]
		if counter.referenceCount > 0 || !counter.flushedToDisk {
			// Sanity check, should be impossible
			return fmt.Errorf("expected view to be released and flushed, refcount = %d, flushed = %t",
				counter.referenceCount, counter.flushedToDisk)
		}
		delete(c.versionMap, targetBlockHeight)
	}
	c.oldestBlockHeight = lastBlockHeight

	return nil
}

// EscapeHatchUnderlyingDB returns the raw backing database, bypassing every guarantee this manager
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
func (c *viewManager) EscapeHatchUnderlyingDB() types.KeyValueDB {
	return c.db
}

func (c *viewManager) Name() string {
	return c.config.Name
}

// Close is idempotent: teardown runs exactly once and subsequent calls return the same result.
func (c *viewManager) Close() error {
	c.closeOnce.Do(func() {
		c.closeErr = c.closeInternal()
	})
	return c.closeErr
}

func (c *viewManager) closeInternal() error {
	// Tell the lifecycle runner to exit, then wait for it to report offline. The send is
	// buffered, so it does not block when the runner has already exited (manager failure), and
	// the runner is guaranteed to close lifecycleExited (its defer runs even on panic).
	c.lifecycleExit <- struct{}{}
	<-c.lifecycleExited

	// Release everyone blocked on the manager's future: AwaitFlush, backpressured
	// View callers, and reads still awaiting results. The cancel happens under versionLock
	// because backpressure waiters re-check the context under that lock before parking on the
	// cond; a lockless cancel could slip between a waiter's check and its Wait, losing the
	// wakeup.
	c.versionLock.Lock()
	c.cancel()

	// Stop serving reads and accepting writes, the same as a brick does: the runner is gone, so any
	// write accepted from here on could never be flushed. First failure wins inside the shard, so a
	// brick that already ran keeps reporting its own cause rather than ErrViewManagerClosed.
	for _, s := range c.shards {
		s.TakeOutOfService(ErrViewManagerClosed)
	}
	c.versionLock.Unlock()
	c.lifecycleBackpressureCond.Broadcast()

	// Wait for the metrics scrape loop (if any) to observe the cancellation and exit.
	c.metrics.awaitStopped()

	// Name any iterator still open, before the database it reads goes away. Best-effort: the report
	// does not block, and the count may be stale. See the Close contract on ViewManager.
	leakedErr := c.assertNoLeakedIterators()

	// The manager owns the database, so it closes it. This happens after the lifecycle runner has
	// reported offline, so no flush can still be in flight against it.
	dbErr := c.db.Close()

	c.versionLock.Lock()
	defer c.versionLock.Unlock()
	if c.fatalErr != nil {
		return errors.Join(fmt.Errorf("view manager failed: %w", c.fatalErr), leakedErr)
	}
	if dbErr != nil {
		return errors.Join(fmt.Errorf("close underlying database: %w", dbErr), leakedErr)
	}
	return leakedErr
}

// assertNoLeakedIterators checks that every iterator handed out has been closed, returning an error
// naming the count when any are still open. Returns an error rather than panicking: the caller folds
// it into whatever else Close reports.
//
// Every iterator registers with every shard, so any one shard's count is the manager's count; shard 0
// is read under its own lock. The manager always has at least one shard (the config requires it).
func (c *viewManager) assertNoLeakedIterators() error {
	s := c.shards[0]
	s.lock.RLock()
	open := s.openIterators
	s.lock.RUnlock()

	if open == 0 {
		return nil
	}
	return fmt.Errorf("manager %q closed with %d iterator(s) still open; reading them is undefined "+
		"behaviour, and this is a leak in the caller", c.config.Name, open)
}
