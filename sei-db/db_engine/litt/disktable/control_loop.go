package disktable

import (
	"fmt"
	"log/slog"
	"sync"
	"sync/atomic"
	"time"

	"github.com/sei-protocol/sei-chain/sei-db/db_engine/litt"
	"github.com/sei-protocol/sei-chain/sei-db/db_engine/litt/disktable/keymap"
	"github.com/sei-protocol/sei-chain/sei-db/db_engine/litt/disktable/segment"
	"github.com/sei-protocol/sei-chain/sei-db/db_engine/litt/metrics"
	"github.com/sei-protocol/sei-chain/sei-db/db_engine/litt/types"
	"github.com/sei-protocol/sei-chain/sei-db/db_engine/litt/util"
)

// controlLoop runs a goroutine that handles control messages for the disk table.
type controlLoop struct {
	logger *slog.Logger

	// diskTable is the disk table that this control loop is associated with.
	diskTable *DiskTable

	// errorMonitor is used to react to fatal errors anywhere in the disk table.
	errorMonitor *util.ErrorMonitor

	// controllerChannel is the channel for messages sent to the control loop.
	controllerChannel chan any

	// The index of the lowest numbered segment. After initial creation, only the garbage collection
	// thread is permitted to read/write this value for the sake of thread safety.
	lowestSegmentIndex uint32

	// The index of the highest numbered segment. All writes are applied to this segment.
	highestSegmentIndex uint32

	// This value mirrors highestSegmentIndex, but is thread safe to read from external goroutines.
	// There are several unit tests that read this value, and so there needs to be a threadsafe way
	// to access it. Since new segments are added on an infrequent basis and this is never read in
	// production, maintaining this atomic variable has negligible overhead.
	threadsafeHighestSegmentIndex atomic.Uint32

	// segmentLock protects access to the variables segments and highestSegmentIndex.
	// Does not protect the segments themselves.
	segmentLock sync.RWMutex

	// All segments currently in use. Only the control loop modifies this map, but other threads may read from it.
	// The control loop does not need to hold a lock when doing read operations on this map, since no other thread
	// will modify it. The control loop does need to hold a lock when modifying this map, though, and other threads
	// must hold a lock when reading from it.
	segments map[uint32]*segment.Segment

	// The number of bytes contained within the immutable segments. This tracks the number of bytes that are
	// on disk, not bytes in memory. For thread safety, this variable may only be read/written in the constructor
	// and in the control loop.
	immutableSegmentSize uint64

	// The target size for value files.
	targetFileSize uint32

	// The maximum number of keys in a segment.
	maxKeyCount uint32

	// The target size for key files.
	targetKeyFileSize uint64

	// The size of the disk table is stored here.
	size *atomic.Uint64

	// The number of keys in the table.
	keyCount *atomic.Int64

	// clock is the time source used by the disk table.
	clock func() time.Time

	// The locations where segment files are stored.
	segmentPaths []*segment.SegmentPath

	// Controls if snapshotting is enabled or not.
	snapshottingEnabled bool

	// whether fsync mode is enabled.
	fsync bool

	// If true, then the control loop has been stopped.
	stopped atomic.Bool

	// Encapsulates metrics for the database.
	metrics *metrics.LittDBMetrics

	// The table's name.
	name string

	// The keymap used to store key-to-address mappings.
	keymap keymap.Keymap

	// The goroutine responsible for asynchronously applying keymap mutations. GC schedules keymap deletes onto
	// it; the control loop drains it on shutdown and writes the final sealed segment's keys through it.
	keymapManager *keymapManager

	// The goroutine responsible for blocking on flush operations.
	flushLoop *flushLoop

	// garbageCollectionPeriod is the period at which garbage collection is run.
	garbageCollectionPeriod time.Duration

	// gcFilter, if non-nil, is consulted before a TTL-expired segment is deleted. A segment may only be
	// deleted once gcFilter returns true for every key in its key file. If nil, only TTL determines GC
	// eligibility. Only invoked from the control loop goroutine.
	gcFilter litt.GCFilter

	// The following three fields form a resumable cursor used by gcFilter scanning. When gcFilter blocks
	// (returns false) on a key, GC stops and remembers its position so that the next GC pass resumes where
	// it left off instead of re-scanning keys already known to pass. The cursor is scoped to a single
	// segment (always the current lowestSegmentIndex, since segments are deleted strictly in order), so it
	// self-invalidates when the lowest segment advances.

	// gcCursorSegment is the segment index the cursor currently refers to.
	gcCursorSegment uint32

	// gcCursorKeys caches the keys for gcCursorSegment, read once and reused across passes. A sealed
	// segment's key file is immutable, so this cache is always safe. nil means no keys are loaded.
	gcCursorKeys []*types.ScopedKey

	// gcCursorIndex is the index into gcCursorKeys of the next key to test with gcFilter.
	gcCursorIndex int

	// openIteratorCount is the number of currently-open iterators. Garbage collection is suspended while
	// this is greater than zero (see Table.Iterator). Only the control loop goroutine touches this field.
	openIteratorCount int

	// newestPrimaryKey is a copy of the primary key of the most recently written Put. Used to serve
	// GetNewestKey without sealing or reading from disk. Only the control loop goroutine touches this field.
	newestPrimaryKey []byte

	// mutableFirstPrimaryKey is a copy of the primary key of the first Put written to the current mutable
	// segment (nil if the mutable segment has received no writes). It lets GetOldestKey report the oldest
	// key even when the lowest remaining segment is the (unsealed) mutable segment, where GetKeys cannot be
	// used. Only the control loop goroutine touches this field.
	mutableFirstPrimaryKey []byte

	// deletionWatermarkChan receives deletion-watermark updates published by the keymap manager (see
	// keymapManager.publishDeletionWatermark). The control loop drains it into keymapDeletionWatermark before
	// each GC pass.
	deletionWatermarkChan chan int64

	// keymapDeletionWatermark is the highest segment index whose keymap entries the manager has durably
	// deleted, as last reported. Phase 2 of GC deletes a segment's files only once this watermark covers it.
	// Only the control loop goroutine touches this field.
	keymapDeletionWatermark int64

	// deletionScheduledIndex is the highest segment index whose keymap-entry deletion has been scheduled on the
	// keymap manager (phase 1 of GC). Phase 1 resumes scheduling from just above it. Only the control loop
	// goroutine touches this field.
	deletionScheduledIndex int64
}

// enqueue enqueues a request to the control loop. Returns an error if the request could not be sent due to the
// database being in a panicked state. Only types defined in control_loop_messages.go are permitted to be sent
// to the control loop.
func (c *controlLoop) enqueue(request controlLoopMessage) error {
	return util.Send(c.errorMonitor, c.controllerChannel, request)
}

// run runs the control loop for the disk table. It has sole responsibility for scheduling all operations that
// mutate the data in the disk table.
func (c *controlLoop) run() {
	ticker := time.NewTicker(c.garbageCollectionPeriod)
	defer ticker.Stop()

	for {
		select {
		case <-c.errorMonitor.ImmediateShutdownRequired():
			c.diskTable.logger.Info("context done, shutting down disk table control loop")
			return
		case message := <-c.controllerChannel:
			if req, ok := message.(*controlLoopWriteRequest); ok {
				c.handleWriteRequest(req)
			} else if req, ok := message.(*controlLoopFlushRequest); ok {
				c.handleFlushRequest(req)
			} else if req, ok := message.(*controlLoopSetShardingFactorRequest); ok {
				c.handleControlLoopSetShardingFactorRequest(req)
			} else if req, ok := message.(*controlLoopShutdownRequest); ok {
				c.handleShutdownRequest(req)
				return
			} else if req, ok := message.(*controlLoopGCRequest); ok {
				if req.deleteFilesOnly {
					// RunGC's second pass: phase 2 only (delete the files of segments the manager has
					// finished deleting from the keymap). Skipping phase 1 means the GC filter is not
					// evaluated a second time per RunGC() call.
					if c.openIteratorCount == 0 {
						c.refreshDeletionWatermark()
						c.deleteCollectedSegments()
						c.updateCurrentSize()
					}
				} else {
					c.doGarbageCollection()
				}
				req.completionChan <- struct{}{}
			} else if req, ok := message.(*controlLoopOpenIteratorRequest); ok {
				c.handleOpenIteratorRequest(req)
			} else if req, ok := message.(*controlLoopCloseIteratorRequest); ok {
				c.handleCloseIteratorRequest(req)
			} else if req, ok := message.(*controlLoopBoundaryKeysRequest); ok {
				c.handleBoundaryKeysRequest(req)
			} else {
				c.errorMonitor.Panic(fmt.Errorf("unknown control message type %T", message))
				return
			}
		case <-ticker.C:
			c.doGarbageCollection()
		}
	}
}

// doGarbageCollection runs one garbage-collection pass. GC is two-phase so that a segment's files are never
// removed before its keymap entries are durably deleted (a crash in the other order would leave keymap
// entries pointing at deleted files, which repair cannot fix). Phase 2 deletes the files of segments whose
// keymap entries the keymap manager has already durably removed; phase 1 schedules the keymap-entry deletion
// of newly-expired segments onto the manager. Each pass is cheap on the control loop (the keymap I/O happens
// on the manager), so GC can run frequently; a segment is fully collected over consecutive passes.
func (c *controlLoop) doGarbageCollection() {
	if c.openIteratorCount > 0 {
		// GC is suspended while one or more iterators are open. Deleting a segment (or its keymap entries) out
		// from under an open iterator would corrupt its snapshot, so we skip GC until every iterator is closed.
		return
	}

	start := c.clock()
	defer func() {
		if c.metrics != nil {
			c.metrics.ReportGarbageCollectionLatency(c.name, c.clock().Sub(start))
		}
		c.updateCurrentSize()
	}()

	// Pick up the latest deletion watermark published by the keymap manager.
	c.refreshDeletionWatermark()

	// Phase 2: delete the files of segments whose keymap entries are durably deleted.
	c.deleteCollectedSegments()

	// Phase 1: schedule keymap-entry deletion for newly-expired segments. Skipped when no TTL is configured
	// (nothing new expires), but phase 2 above still drains any deletions scheduled before TTL was cleared.
	ttl := c.diskTable.getTTL()
	if ttl.Nanoseconds() <= 0 {
		return
	}
	c.scheduleExpiredSegmentDeletions(start, ttl)
}

// deleteCollectedSegments (phase 2) deletes the files of every segment at or below the deletion watermark —
// i.e. every segment whose keymap entries the manager has durably removed. The segment is released for async
// file deletion (reservation holders keep it alive until they finish reading) and removed from the live set.
func (c *controlLoop) deleteCollectedSegments() {
	for int64(c.lowestSegmentIndex) <= c.keymapDeletionWatermark {
		index := c.lowestSegmentIndex
		seg := c.segments[index]

		if seg.Size() > c.immutableSegmentSize {
			c.logger.Error("segment size larger than immutable segment size, reported DB size will not be accurate",
				"segment", index,
				"size", seg.Size(),
				"limit", c.immutableSegmentSize,
			)
		}

		c.immutableSegmentSize -= seg.Size()
		c.keyCount.Add(-1 * int64(seg.KeyCount()))

		// Deletion of segment files happens once the segment is released by all reservation holders.
		seg.Release()
		c.segmentLock.Lock()
		delete(c.segments, index)
		c.segmentLock.Unlock()

		c.lowestSegmentIndex++
	}
}

// scheduleExpiredSegmentDeletions (phase 1) walks sealed, TTL-expired segments that have not yet been
// scheduled for deletion and sends their keys to the keymap manager for deletion, advancing
// deletionScheduledIndex. A GC filter, if configured, can block a segment (and therefore every later one);
// the resume cursor remembers where to continue on the next pass.
func (c *controlLoop) scheduleExpiredSegmentDeletions(start time.Time, ttl time.Duration) {
	index := c.lowestSegmentIndex
	if c.deletionScheduledIndex >= int64(index) {
		// deletionScheduledIndex is a segment index in [lowestSegmentIndex, highestSegmentIndex] here, so it
		// fits a uint32.
		index = uint32(c.deletionScheduledIndex) + 1 //nolint:gosec // bounded segment index, fits uint32
	}

	for ; index <= c.highestSegmentIndex; index++ {
		seg := c.segments[index]
		if !seg.IsSealed() {
			// Can't delete the (unsealed) mutable segment.
			return
		}

		if start.Sub(seg.GetSealTime()) < ttl {
			// Not old enough; since segments expire in order, neither is any later one, so stop this pass.
			return
		}

		// Load the segment's keys once and cache them while it remains the segment under consideration. A
		// sealed segment's key file is immutable, so the cache stays valid across passes.
		if c.gcCursorKeys == nil || c.gcCursorSegment != index {
			keys, err := seg.GetKeys()
			if err != nil {
				c.errorMonitor.Panic(fmt.Errorf("failed to get keys: %w", err))
				return
			}
			c.gcCursorKeys = keys
			c.gcCursorSegment = index
			c.gcCursorIndex = 0
		}

		// If a GC filter is configured, the segment may only be deleted once the filter returns true for every
		// key. Walk from where the previous pass left off; if the filter blocks, keep the cursor and stop.
		if c.gcFilter != nil {
			for ; c.gcCursorIndex < len(c.gcCursorKeys); c.gcCursorIndex++ {
				sk := c.gcCursorKeys[c.gcCursorIndex]
				ok, err := c.gcFilter(sk.Key, sk.Kind.IsPrimary())
				if err != nil {
					c.errorMonitor.Panic(fmt.Errorf("gc filter failed: %w", err))
					return
				}
				if !ok {
					// The filter blocks this key, and therefore this and all later segments, for this pass.
					return
				}
			}
		}

		// Schedule the segment's keymap entries for deletion. The manager applies it asynchronously and then
		// advances the deletion watermark, which lets a later phase 2 delete this segment's files.
		if err := c.keymapManager.scheduleDelete(c.gcCursorKeys, int64(index)); err != nil {
			// The only error path is a panic-induced shutdown; nothing more to do.
			return
		}
		c.deletionScheduledIndex = int64(index)

		// Reset the cursor for the next segment.
		c.gcCursorKeys = nil
		c.gcCursorIndex = 0
	}
}

// refreshDeletionWatermark drains all deletion-watermark updates published by the keymap manager and advances
// the locally-tracked watermark to the highest value seen. The manager may publish several updates (or drop
// some when its channel is full); draining to the maximum yields the freshest available watermark.
func (c *controlLoop) refreshDeletionWatermark() {
	for {
		select {
		case watermark := <-c.deletionWatermarkChan:
			if watermark > c.keymapDeletionWatermark {
				c.keymapDeletionWatermark = watermark
			}
		default:
			return
		}
	}
}

// handleOpenIteratorRequest handles a request to open an iterator. It seals the current mutable segment
// (if it has any keys) so that all keys in scope are readable, suspends garbage collection by
// incrementing the open-iterator count, and returns the ordered snapshot of sealed segments the iterator
// will walk.
//
// Reservations are not taken on the snapshot segments: garbage collection is fully suspended for the
// entire lifetime of the iterator (while openIteratorCount > 0), so no segment in the snapshot can be
// deleted before the iterator is closed.
func (c *controlLoop) handleOpenIteratorRequest(req *controlLoopOpenIteratorRequest) {
	// Seal the current mutable segment so that the keys written so far are readable. Skip the seal if the
	// mutable segment is empty, to avoid accumulating empty sealed segments when iterators are opened
	// against an idle table.
	if c.segments[c.highestSegmentIndex].KeyCount() > 0 {
		err := c.expandSegments()
		if err != nil {
			c.errorMonitor.Panic(fmt.Errorf("failed to seal mutable segment for iterator: %w", err))
			return
		}
	}

	// The in-scope segments are all sealed segments: [lowestSegmentIndex, highestSegmentIndex). The
	// highest segment is the (now empty) mutable segment and is excluded.
	segs := make([]*segment.Segment, 0, c.highestSegmentIndex-c.lowestSegmentIndex)
	for index := c.lowestSegmentIndex; index < c.highestSegmentIndex; index++ {
		segs = append(segs, c.segments[index])
	}

	c.openIteratorCount++
	c.metrics.ReportOpenIteratorCount(c.name, int64(c.openIteratorCount))

	req.responseChan <- segs
}

// handleCloseIteratorRequest handles a request to close an iterator, decrementing the open-iterator count
// and thereby re-enabling garbage collection once the last iterator is closed.
func (c *controlLoop) handleCloseIteratorRequest(req *controlLoopCloseIteratorRequest) {
	if c.openIteratorCount > 0 {
		c.openIteratorCount--
	}
	c.metrics.ReportOpenIteratorCount(c.name, int64(c.openIteratorCount))
	req.completionChan <- struct{}{}
}

// handleBoundaryKeysRequest handles a request for the oldest and newest primary keys.
func (c *controlLoop) handleBoundaryKeysRequest(req *controlLoopBoundaryKeysRequest) {
	resp := &boundaryKeysResponse{}

	oldest, oldestExists, err := c.computeOldestPrimaryKey()
	if err != nil {
		resp.err = fmt.Errorf("failed to compute oldest primary key: %w", err)
		req.responseChan <- resp
		return
	}
	resp.oldestKey = oldest
	resp.oldestExists = oldestExists

	// The table is non-empty iff it has an oldest primary key, so newest existence mirrors oldest
	// existence. Both are derived from control-loop state (not the optimistically-updated keyCount,
	// which is bumped by the caller before the write is processed and reconstructed at startup before
	// any write), so they stay consistent with the writes the control loop has actually processed.
	if oldestExists {
		newest, err := c.computeNewestPrimaryKey()
		if err != nil {
			resp.err = fmt.Errorf("failed to compute newest primary key: %w", err)
			req.responseChan <- resp
			return
		}
		resp.newestKey = newest
		resp.newestExists = true
	}

	req.responseChan <- resp
}

// computeNewestPrimaryKey returns the newest (most recently inserted) primary key. The most recent write
// of the current session is tracked in memory by handleWriteRequest; if no write has occurred this
// session (e.g. immediately after a restart, when newestPrimaryKey is nil but data exists on disk), the
// newest key is recovered from the highest sealed segment. Only meaningful when the table is known to be
// non-empty (oldestExists); under that precondition the recovery walk always finds a primary key.
func (c *controlLoop) computeNewestPrimaryKey() ([]byte, error) {
	if c.newestPrimaryKey != nil {
		return c.newestPrimaryKey, nil
	}

	// No write has been processed this session, so the mutable (highest) segment is empty and the
	// newest key lives in the highest sealed segment. Walk downward and return its last primary key.
	for index := c.highestSegmentIndex; ; index-- {
		seg := c.segments[index]
		if seg.IsSealed() {
			keys, err := seg.GetKeys()
			if err != nil {
				return nil, fmt.Errorf("failed to get keys for segment %d: %w", index, err)
			}
			for i := len(keys) - 1; i >= 0; i-- {
				if keys[i].Kind.IsPrimary() {
					return keys[i].Key, nil
				}
			}
		}
		if index == c.lowestSegmentIndex {
			break
		}
	}

	return nil, nil
}

// computeOldestPrimaryKey returns the oldest non-deleted primary key in the table. It walks segments from
// the lowest index upward, returning the first primary key it finds. Sealed segments are read via
// GetKeys; if the lowest remaining segment is the (unsealed) mutable segment, the in-memory
// mutableFirstPrimaryKey is used instead.
func (c *controlLoop) computeOldestPrimaryKey() ([]byte, bool, error) {
	for index := c.lowestSegmentIndex; index <= c.highestSegmentIndex; index++ {
		seg := c.segments[index]

		if !seg.IsSealed() {
			// The mutable segment cannot be read via GetKeys; fall back to the tracked first key.
			if c.mutableFirstPrimaryKey != nil {
				return c.mutableFirstPrimaryKey, true, nil
			}
			return nil, false, nil
		}

		keys, err := seg.GetKeys()
		if err != nil {
			return nil, false, fmt.Errorf("failed to get keys for segment %d: %w", index, err)
		}
		for _, key := range keys {
			if key.Kind.IsPrimary() {
				return key.Key, true, nil
			}
		}
	}

	return nil, false, nil
}

// getReservedSegment returns the segment with the given index. Segment is reserved, and it is the caller's
// responsibility to release the reservation when done. Returns true if the segment was found and reserved,
// and false if the segment could not be found or could not be reserved.
func (c *controlLoop) getReservedSegment(index uint32) (*segment.Segment, bool) {
	c.segmentLock.RLock()
	defer c.segmentLock.RUnlock()

	seg, ok := c.segments[index]
	if !ok {
		return nil, false
	}

	ok = seg.Reserve()
	if !ok {
		// segmented was deleted out from under us
		return nil, false
	}

	return seg, true
}

// getSegments returns the segments of the disk table. It is only legal to call this after the control loop has been
// stopped.
func (c *controlLoop) getSegments() (map[uint32]*segment.Segment, error) {
	if !c.stopped.Load() {
		return nil, fmt.Errorf("cannot get segments until control loop has stopped")
	}
	return c.segments, nil
}

// updateCurrentSize updates the size of the table.
func (c *controlLoop) updateCurrentSize() {
	size := c.immutableSegmentSize +
		c.segments[c.highestSegmentIndex].Size()

	c.size.Store(size)
}

// handleWriteRequest handles a controlLoopWriteRequest control message.
func (c *controlLoop) handleWriteRequest(req *controlLoopWriteRequest) {
	for _, kv := range req.values {
		// Do the write.
		seg := c.segments[c.highestSegmentIndex]

		// Track boundary keys. The newest primary key is simply the most recently written key. The
		// mutable segment's first primary key is recorded the first time a key is written to a fresh
		// mutable segment (it is reset to nil whenever a new mutable segment is created).
		if c.mutableFirstPrimaryKey == nil {
			c.mutableFirstPrimaryKey = kv.Key
		}
		c.newestPrimaryKey = kv.Key

		keyCount, keyFileSize, err := seg.Write(kv)
		shardSize := seg.GetMaxShardSize()
		if err != nil {
			c.errorMonitor.Panic(
				fmt.Errorf("failed to write to segment %d: %w", c.highestSegmentIndex, err))
			return
		}

		// Check to see if the write caused the mutable segment to become full.
		if shardSize > uint64(c.targetFileSize) || keyCount >= c.maxKeyCount || keyFileSize >= c.targetKeyFileSize {
			// Mutable segment is full. Before continuing, we need to expand the segments.
			err = c.expandSegments()
			if err != nil {
				c.errorMonitor.Panic(fmt.Errorf("failed to expand segments: %w", err))
				return
			}
		}
	}

	c.updateCurrentSize()
}

// expandSegments seals the latest segment and creates a new mutable segment.
func (c *controlLoop) expandSegments() error {
	now := c.clock()

	// Seal the previous segment.
	flushLoopResponseChan := make(chan struct{}, 1)
	request := &flushLoopSealRequest{
		now:           now,
		segmentToSeal: c.segments[c.highestSegmentIndex],
		responseChan:  flushLoopResponseChan,
	}
	err := c.flushLoop.enqueue(request)
	if err != nil {
		return fmt.Errorf("failed to send seal request: %w", err)
	}

	// Unfortunately, it is necessary to block until the sealing has been completed. Although this may result
	// in a brief interruption in new write work being sent to the segment, expanding the number of segments is
	// infrequent, even for very high throughput workloads.
	_, err = util.Await(c.errorMonitor, flushLoopResponseChan)
	if err != nil {
		return fmt.Errorf("failed to seal segment: %w", err)
	}

	// Record the size of the segment.
	c.immutableSegmentSize += c.segments[c.highestSegmentIndex].Size()

	// Create a new segment.
	newSegment, err := segment.CreateSegment(
		c.logger,
		c.errorMonitor,
		c.highestSegmentIndex+1,
		c.segmentPaths,
		c.snapshottingEnabled,
		c.diskTable.getShardingFactor(),
		c.fsync)
	if err != nil {
		return err
	}
	c.segments[c.highestSegmentIndex].SetNextSegment(newSegment)
	c.highestSegmentIndex++
	c.threadsafeHighestSegmentIndex.Add(1)

	c.segmentLock.Lock()
	c.segments[c.highestSegmentIndex] = newSegment
	c.segmentLock.Unlock()

	// The new mutable segment has received no writes yet, so it has no first primary key.
	c.mutableFirstPrimaryKey = nil

	c.updateCurrentSize()

	return nil
}

// handleFlushRequest handles the part of the flush that is performed on the control loop.
// The control loop is responsible for enqueuing the flush request in the segment's work queue (thus
// ensuring a serial ordering with respect to other operations on the control loop), but not for
// waiting for the segment to finish the flush.
func (c *controlLoop) handleFlushRequest(req *controlLoopFlushRequest) {
	// This method will enqueue a flush operation within the segment. Once that is done,
	// it becomes the responsibility of the flush loop to wait for the flush to complete.
	flushWaitFunction, err := c.segments[c.highestSegmentIndex].Flush()
	if err != nil {
		c.errorMonitor.Panic(fmt.Errorf("failed to flush segment %d: %w", c.highestSegmentIndex, err))
		return
	}

	// The flush loop is responsible for the remaining parts of the flush.
	request := &flushLoopFlushRequest{
		flushWaitFunction: flushWaitFunction,
		responseChan:      req.responseChan,
	}
	err = c.flushLoop.enqueue(request)
	if err != nil {
		c.logger.Error("failed to send flush request to flush loop", "error", err)
	}
}

// handleControlLoopSetShardingFactorRequest updates the sharding factor of the disk table. If the requested
// sharding factor is the same as before, no action is taken. If it is different, the sharding factor is updated,
// the current mutable segment is sealed, and a new mutable segment is created.
func (c *controlLoop) handleControlLoopSetShardingFactorRequest(req *controlLoopSetShardingFactorRequest) {

	if req.shardingFactor == c.diskTable.getShardingFactor() {
		// No action necessary.
		return
	}
	c.diskTable.setShardingFactor(req.shardingFactor)

	// This seals the current mutable segment and creates a new one. The new segment will have the new sharding factor.
	err := c.expandSegments()
	if err != nil {
		c.errorMonitor.Panic(fmt.Errorf("failed to expand segments: %w", err))
		return
	}
}

// handleShutdownRequest performs tasks necessary to cleanly shut down the disk table.
func (c *controlLoop) handleShutdownRequest(req *controlLoopShutdownRequest) {
	// Instruct the flush loop to stop.
	shutdownCompleteChan := make(chan struct{})
	request := &flushLoopShutdownRequest{
		shutdownCompleteChan: shutdownCompleteChan,
	}
	err := c.flushLoop.enqueue(request)
	if err != nil {
		c.logger.Error("failed to send shutdown request to flush loop", "error", err)
		return
	}

	_, err = util.Await(c.errorMonitor, shutdownCompleteChan)
	if err != nil {
		c.logger.Error("failed to shutdown flush loop", "error", err)
		return
	}

	// Drain the keymap writer. The flush loop has stopped, so no new keymap writes will be scheduled; draining
	// applies every write already scheduled (and prunes the unflushed data cache) before we seal the final
	// segment and stop the keymap. This is what keeps a clean shutdown fully consistent: the next startup's
	// repair has nothing to rescue.
	err = c.keymapManager.drain()
	if err != nil {
		c.logger.Error("failed to drain keymap writer", "error", err)
		return
	}

	// Seal the mutable segment
	durableKeys, err := c.segments[c.highestSegmentIndex].Seal(c.clock())
	if err != nil {
		c.errorMonitor.Panic(fmt.Errorf("failed to seal mutable segment: %w", err))
		return
	}

	// Write the keys that are now durable in the segment into the keymap. The keymap writer has been drained
	// above, so its goroutine has exited and this synchronous call cannot race with it.
	err = c.keymapManager.writeBatch(durableKeys)
	if err != nil {
		c.errorMonitor.Panic(fmt.Errorf("failed to flush keys: %w", err))
		return
	}

	// Stop the keymap
	err = c.keymap.Stop()
	if err != nil {
		c.errorMonitor.Panic(fmt.Errorf("failed to stop keymap: %w", err))
		return
	}

	c.stopped.Store(true)
	req.shutdownCompleteChan <- struct{}{}
}
