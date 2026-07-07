package disktable

import (
	"fmt"
	"log/slog"
	"math"
	"sync"
	"sync/atomic"
	"time"

	"github.com/sei-protocol/sei-chain/sei-db/db_engine/litt/disktable/keymap"
	"github.com/sei-protocol/sei-chain/sei-db/db_engine/litt/disktable/segment"
	"github.com/sei-protocol/sei-chain/sei-db/db_engine/litt/metrics"
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

	// The index of the lowest numbered segment. It is advanced only by the control loop, in
	// deleteEligibleSegments, as collected segments' files are removed. Only the control loop goroutine touches it.
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

	// autoFlushByteThreshold is the number of value bytes written through the control loop (without an
	// intervening flush) that triggers an automatic fire-and-forget flush, bounding the in-memory
	// unflushed-data cache. Set once at construction from Config.AutoFlushByteThreshold; never mutated.
	autoFlushByteThreshold uint64

	// bytesSinceLastFlush accumulates value bytes written since the last flush (explicit or automatic).
	// Only the control loop goroutine reads or writes it, so it needs no synchronization.
	bytesSinceLastFlush uint64

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

	// garbageCollectionPeriod is the period at which the control loop reclaims collected segments' files. The GC
	// manager schedules the collection itself on its own ticker of the same period.
	garbageCollectionPeriod time.Duration

	// openIteratorCount is the number of currently-open iterators, maintained only for the open-iterator metric.
	// Iterators pin their snapshot segments via reservations rather than by suspending GC, so this no longer
	// gates collection. Touched only by the control loop (on iterator open/close).
	openIteratorCount int

	// newestPrimaryKey is a copy of the primary key of the most recently written Put. Used to serve
	// GetNewestKey without sealing or reading from disk. Only the control loop goroutine touches this field.
	newestPrimaryKey []byte

	// mutableFirstPrimaryKey is a copy of the primary key of the first Put written to the current mutable
	// segment (nil if the mutable segment has received no writes). It lets GetOldestKey report the oldest
	// key even when the lowest remaining segment is the (unsealed) mutable segment, where GetKeys cannot be
	// used. Only the control loop goroutine touches this field.
	mutableFirstPrimaryKey []byte

	// deletionWatermarkChan receives deletion-watermark updates from the keymap manager, which writes them via
	// publishDeletionWatermark. The control loop owns this channel and drains it into keymapDeletionWatermark
	// (via refreshDeletionWatermark) before each GC pass and before serving iterator-open and boundary-key reads,
	// so those reads floor at the freshest readable segment. It is a latest-value channel: publishDeletionWatermark
	// uses drain-then-send so the freshest (highest) monotonic watermark is never the one dropped on overflow.
	deletionWatermarkChan chan int64

	// keymapDeletionWatermark is the highest segment index whose keymap entries the manager has durably
	// deleted, as last reported. The control loop deletes a segment's files only once this watermark covers it.
	// Only the control loop goroutine touches this field.
	keymapDeletionWatermark int64
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
				// Delete the files of segments whose keymap entries the manager has finished deleting. The
				// collection that schedules those deletes runs on the GC manager; RunGC drives it separately.
				c.deleteEligibleSegments()
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
			c.deleteEligibleSegments()
		}
	}
}

// deleteEligibleSegments picks up the latest deletion watermark published by the keymap manager and reclaims the
// files of every segment at or below it — i.e. every segment whose keymap entries the manager has already durably
// removed.
func (c *controlLoop) deleteEligibleSegments() {
	defer c.updateCurrentSize()

	// Pick up the latest deletion watermark published by the keymap manager, then delete the files of every
	// segment it now covers.
	c.refreshDeletionWatermark()

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
		c.lowestSegmentIndex++
		c.segmentLock.Unlock()
	}
}

// publishDeletionWatermark records that every segment up to and including watermark has had its keymap entries
// durably deleted, so the control loop may reclaim their files. The watermark channel is latest-value: any stale
// buffered watermark is discarded before the fresher one is enqueued, so an overflow never drops the newest
// (highest) value, leaving readableFloor stuck below the true durable-delete frontier. This is safe because there
// is a single producer (the keymap-manager goroutine), the published watermarks are strictly monotonic, and the
// single consumer (the control loop) takes the max on drain. Both sends are non-blocking, so the keymap manager
// never blocks on the control loop (which would risk deadlocking the seal path). Mirrors gcManager.setTTL.
func (c *controlLoop) publishDeletionWatermark(watermark int64) {
	// Discard any stale buffered watermark, then enqueue the fresher one.
	select {
	case <-c.deletionWatermarkChan:
	default:
	}
	select {
	case c.deletionWatermarkChan <- watermark:
	default:
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

// readableFloor returns the lowest segment index that is still logically readable. Segments below it have had
// their keymap entries durably deleted (their files may linger in the segments map until the control loop
// reclaims them).
func (c *controlLoop) readableFloor() uint32 {
	return uint32(c.keymapDeletionWatermark + 1) //nolint:gosec // watermark >= -1, so floor >= 0
}

// handleOpenIteratorRequest handles a request to open an iterator. It seals the current mutable segment
// (if it has any keys) so that all keys in scope are readable, reserves each segment the iterator will walk,
// and returns the ordered snapshot of those sealed segments.
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

	// The in-scope segments are the sealed, still-readable segments: [readableFloor, highestSegmentIndex).
	// The highest segment is the (now empty) mutable segment and is excluded.
	c.refreshDeletionWatermark()
	floor := c.readableFloor()
	segs := make([]*segment.Segment, 0, c.highestSegmentIndex-floor)
	for index := floor; index < c.highestSegmentIndex; index++ {
		seg := c.segments[index]
		if !seg.Reserve() {
			c.errorMonitor.Panic(fmt.Errorf("failed to reserve segment %d for iterator", index))
			return
		}
		segs = append(segs, seg)
	}

	c.openIteratorCount++
	c.metrics.ReportOpenIteratorCount(c.name, int64(c.openIteratorCount))

	req.responseChan <- segs
}

// handleCloseIteratorRequest handles a request to close an iterator. The iterator releases its segment
// reservations itself (on Close); this only updates the open-iterator metric.
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

	// Pick up the freshest deletion watermark so the boundary computations floor at the lowest still-readable
	// segment (readableFloor) and never report a key from a logically-deleted-but-not-yet-reclaimed segment.
	c.refreshDeletionWatermark()

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
	// newest key lives in the highest sealed segment. Walk downward and return its last primary key, stopping
	// at readableFloor so a logically-deleted-but-not-yet-reclaimed segment is never reported.
	floor := c.readableFloor()
	for index := c.highestSegmentIndex; ; index-- {
		seg := c.segments[index]
		if seg == nil {
			// Should be impossible: [floor, highestSegmentIndex] is a contiguous, fully-populated range. Bubble
			// up rather than nil-dereference if that invariant is ever violated.
			return nil, fmt.Errorf("segment %d missing while computing newest primary key", index)
		}
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
		if index == floor {
			break
		}
	}

	return nil, nil
}

// computeOldestPrimaryKey returns the oldest non-deleted primary key in the table. It walks segments from
// the lowest readable index (readableFloor) upward, returning the first primary key it finds. Sealed segments
// are read via GetKeys; if the lowest readable segment is the (unsealed) mutable segment, the in-memory
// mutableFirstPrimaryKey is used instead.
func (c *controlLoop) computeOldestPrimaryKey() ([]byte, bool, error) {
	// Start at readableFloor, not lowestSegmentIndex: segments below the floor have had their keymap entries
	// durably deleted and may linger in the map until reclaimed; reading them would resurrect collected keys.
	for index := c.readableFloor(); index <= c.highestSegmentIndex; index++ {
		seg := c.segments[index]
		if seg == nil {
			// Should be impossible: [readableFloor, highestSegmentIndex] is a contiguous, fully-populated range.
			// Bubble up rather than nil-dereference if that invariant is ever violated.
			return nil, false, fmt.Errorf("segment %d missing while computing oldest primary key", index)
		}

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

		// Roll to a fresh segment before writing if this value's bytes would cross the 2^32 addressable
		// limit of the segment's value files (offsets are stored as uint32, so a value's first byte must
		// sit below 2^32).
		if seg.GetMaxShardSize()+uint64(len(kv.Value)) > math.MaxUint32 {
			if err := c.expandSegments(); err != nil {
				c.errorMonitor.Panic(fmt.Errorf("failed to expand segments: %w", err))
				return
			}
			seg = c.segments[c.highestSegmentIndex]
		}

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

		// Bound the in-memory unflushed-data cache: once enough bytes have been written without an
		// intervening flush, schedule a fire-and-forget flush so the cache drains as keys become durable.
		c.bytesSinceLastFlush += uint64(len(kv.Value))
		if c.bytesSinceLastFlush >= c.autoFlushByteThreshold {
			c.scheduleAutoFlush()
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
	sealedSegment := c.segments[c.highestSegmentIndex]
	c.immutableSegmentSize += sealedSegment.Size()

	// Hand the freshly-sealed segment to the GC manager. It keeps its own local view of sealed segments and
	// considers them for collection; this is the only way it learns of them. Segments are sealed in index order,
	// so they are delivered in order, exactly once. (The final mutable segment is sealed directly in
	// handleShutdownRequest, not here, and is intentionally not handed over: the GC manager is already stopped.)
	if err := c.diskTable.gcManager.registerImmutableSegment(sealedSegment); err != nil {
		return fmt.Errorf("failed to hand sealed segment to gc manager: %w", err)
	}

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

	// An explicit flush drains the unflushed-data cache, so restart the auto-flush accounting.
	c.bytesSinceLastFlush = 0
}

// scheduleAutoFlush schedules a fire-and-forget flush of the mutable segment to bound the in-memory
// unflushed-data cache. It is triggered from the write path once autoFlushByteThreshold bytes have been
// written without an intervening flush. Unlike handleFlushRequest there is no waiting caller, so the
// request carries a nil responseChan (the flush loop skips the completion signal but still schedules the
// keymap write that drains the cache). Called only on the control loop goroutine.
func (c *controlLoop) scheduleAutoFlush() {
	flushWaitFunction, err := c.segments[c.highestSegmentIndex].Flush()
	if err != nil {
		c.errorMonitor.Panic(fmt.Errorf("failed to flush segment %d: %w", c.highestSegmentIndex, err))
		return
	}

	request := &flushLoopFlushRequest{
		flushWaitFunction: flushWaitFunction,
		responseChan:      nil,
	}
	err = c.flushLoop.enqueue(request)
	if err != nil {
		c.logger.Error("failed to send auto-flush request to flush loop", "error", err)
		return
	}

	c.bytesSinceLastFlush = 0
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
	// Stop the GC manager first, so it schedules no more keymap deletes. Otherwise it could enqueue work onto
	// the keymap manager after (or during) the drain below, racing the drain. The GC manager never calls back
	// into the control loop, so stopping it here cannot deadlock.
	if err := c.diskTable.gcManager.stop(); err != nil {
		c.logger.Error("failed to stop gc manager", "error", err)
		return
	}

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
