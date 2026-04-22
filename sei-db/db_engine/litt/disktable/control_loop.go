package disktable

import (
	"fmt"
	"math/rand"
	"sync"
	"sync/atomic"
	"time"

	"github.com/Layr-Labs/eigenda/litt/disktable/keymap"
	"github.com/Layr-Labs/eigenda/litt/disktable/segment"
	"github.com/Layr-Labs/eigenda/litt/metrics"
	"github.com/Layr-Labs/eigenda/litt/util"
	"github.com/Layr-Labs/eigensdk-go/logging"
)

// controlLoop runs a goroutine that handles control messages for the disk table.
type controlLoop struct {
	logger logging.Logger

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

	// The table's metadata.
	metadata *tableMetadata

	// A source of randomness used for generating sharding salt.
	saltShaker *rand.Rand

	// whether fsync mode is enabled.
	fsync bool

	// If true, then the control loop has been stopped.
	stopped atomic.Bool

	// Encapsulates metrics for the database.
	metrics *metrics.LittDBMetrics

	// The table's name.
	name string

	// The maximum number of keys that can be garbage collected in a single batch.
	gcBatchSize uint64

	// The keymap used to store key-to-address mappings.
	keymap keymap.Keymap

	// The goroutine responsible for blocking on flush operations.
	flushLoop *flushLoop

	// garbageCollectionPeriod is the period at which garbage collection is run.
	garbageCollectionPeriod time.Duration
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
			c.diskTable.logger.Infof("context done, shutting down disk table control loop")
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
				c.doGarbageCollection()
				req.completionChan <- struct{}{}
			} else {
				c.errorMonitor.Panic(fmt.Errorf("unknown control message type %T", message))
				return
			}
		case <-ticker.C:
			c.doGarbageCollection()
		}
	}
}

// doGarbageCollection performs garbage collection on all segments, deleting old ones as necessary.
func (c *controlLoop) doGarbageCollection() {
	start := c.clock()
	ttl := c.metadata.GetTTL()
	if ttl.Nanoseconds() <= 0 {
		// No TTL set, so nothing to do.
		return
	}

	defer func() {
		if c.metrics != nil {
			end := c.clock()
			delta := end.Sub(start)
			c.metrics.ReportGarbageCollectionLatency(c.name, delta)

		}
		c.updateCurrentSize()
	}()

	for index := c.lowestSegmentIndex; index <= c.highestSegmentIndex; index++ {
		seg := c.segments[index]
		if !seg.IsSealed() {
			// We can't delete an unsealed segment.
			return
		}

		sealTime := seg.GetSealTime()
		segmentAge := start.Sub(sealTime)
		if segmentAge < ttl {
			// Segment is not old enough to be deleted.
			return
		}

		// Segment is old enough to be deleted.
		keys, err := seg.GetKeys()
		if err != nil {
			c.errorMonitor.Panic(fmt.Errorf("failed to get keys: %w", err))
			return
		}

		for keyIndex := uint64(0); keyIndex < uint64(len(keys)); keyIndex += c.gcBatchSize {
			lastIndex := keyIndex + c.gcBatchSize
			if lastIndex > uint64(len(keys)) {
				lastIndex = uint64(len(keys))
			}
			err = c.keymap.Delete(keys[keyIndex:lastIndex])
			if err != nil {
				c.errorMonitor.Panic(fmt.Errorf("failed to delete keys: %w", err))
				return
			}
		}

		if seg.Size() > c.immutableSegmentSize {
			c.logger.Errorf("segment %d size %d is larger than immutable segment size %d, "+
				"reported DB size will not be accurate", index, seg.Size(), c.immutableSegmentSize)
		}

		c.immutableSegmentSize -= seg.Size()
		c.keyCount.Add(-1 * int64(seg.KeyCount()))

		// Deletion of segment files will happen when the segment is released by all reservation holders.
		seg.Release()
		c.segmentLock.Lock()
		delete(c.segments, index)
		c.segmentLock.Unlock()

		c.lowestSegmentIndex++
	}
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
		c.segments[c.highestSegmentIndex].Size() +
		c.metadata.Size()

	c.size.Store(size)
}

// handleWriteRequest handles a controlLoopWriteRequest control message.
func (c *controlLoop) handleWriteRequest(req *controlLoopWriteRequest) {
	for _, kv := range req.values {
		// Do the write.
		seg := c.segments[c.highestSegmentIndex]
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
	salt := [16]byte{}
	_, err = c.saltShaker.Read(salt[:])
	if err != nil {
		return fmt.Errorf("failed to read salt: %w", err)
	}
	newSegment, err := segment.CreateSegment(
		c.logger,
		c.errorMonitor,
		c.highestSegmentIndex+1,
		c.segmentPaths,
		c.snapshottingEnabled,
		c.metadata.GetShardingFactor(),
		salt,
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
		c.logger.Errorf("failed to send flush request to flush loop: %v", err)
	}
}

// handleControlLoopSetShardingFactorRequest updates the sharding factor of the disk table. If the requested
// sharding factor is the same as before, no action is taken. If it is different, the sharding factor is updated,
// the current mutable segment is sealed, and a new mutable segment is created.
func (c *controlLoop) handleControlLoopSetShardingFactorRequest(req *controlLoopSetShardingFactorRequest) {

	if req.shardingFactor == c.metadata.GetShardingFactor() {
		// No action necessary.
		return
	}
	err := c.metadata.SetShardingFactor(req.shardingFactor)
	if err != nil {
		c.errorMonitor.Panic(fmt.Errorf("failed to set sharding factor: %w", err))
		return
	}

	// This seals the current mutable segment and creates a new one. The new segment will have the new sharding factor.
	err = c.expandSegments()
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
		c.logger.Errorf("failed to send shutdown request to flush loop: %v", err)
		return
	}

	_, err = util.Await(c.errorMonitor, shutdownCompleteChan)
	if err != nil {
		c.logger.Errorf("failed to shutdown flush loop: %v", err)
		return
	}

	// Seal the mutable segment
	durableKeys, err := c.segments[c.highestSegmentIndex].Seal(c.clock())
	if err != nil {
		c.errorMonitor.Panic(fmt.Errorf("failed to seal mutable segment: %w", err))
		return
	}

	// Flush the keys that are now durable in the segment.
	err = c.diskTable.writeKeysToKeymap(durableKeys)
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
