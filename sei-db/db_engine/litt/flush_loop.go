package litt

import (
	"fmt"
	"sync"
	"time"

	"log/slog"

	"github.com/sei-protocol/sei-chain/sei-db/db_engine/litt/keymap"
	"github.com/sei-protocol/sei-chain/sei-db/db_engine/litt/metrics"
	"github.com/sei-protocol/sei-chain/sei-db/db_engine/litt/unflushed"
	"github.com/sei-protocol/sei-chain/sei-db/db_engine/litt/util"
)

// TODO before merge: see if this can be extracted!

// flushLoop is a struct that runs a goroutine that is responsible for blocking on flush operations.
type flushLoop struct {
	logger *slog.Logger

	// the parent disk table
	diskTable *diskTable

	// Caches data that hasn't yet been flushed to disk. We need to report when we flush segment data,
	// as this i sa prereq for data in the unflushed data cache to be evicted.
	unflushedDataCache *unflushed.UnflushedDataCache

	// Manages access/modification of the keymap.
	keymapManager *keymap.KeymapManager

	// Responsible for handling fatal DB errors.
	errorMonitor *util.ErrorMonitor

	// flushChannel is a channel used to enqueue work on the flush loop.
	flushChannel chan any

	// metrics encapsulates metrics for the DB.
	metrics *metrics.LittDBMetrics

	// provides the current time
	clock func() time.Time

	// the name of the table
	name string

	// This file stores the highest segment index that is fully snapshot. Written as segments are sealed
	// and copied to the snapshot directory, read by the external snapshot consumer.
	upperBoundSnapshotFile *BoundaryFile
}

// Creates a new flush loop.
func NewFlushLoop(
	logger *slog.Logger,
	errorMonitor *util.ErrorMonitor,
	diskTable *diskTable,
	unflushedDataCache *unflushed.UnflushedDataCache,
	keymapManager *keymap.KeymapManager,
	clock func() time.Time,
	name string,
	upperBoundSnapshotFile *BoundaryFile,
	m *metrics.LittDBMetrics,
) *flushLoop {
	return &flushLoop{
		logger:                 logger,
		errorMonitor:           errorMonitor,
		diskTable:              diskTable,
		unflushedDataCache:     unflushedDataCache,
		keymapManager:          keymapManager,
		clock:                  clock,
		name:                   name,
		upperBoundSnapshotFile: upperBoundSnapshotFile,
		flushChannel:           make(chan any, 1000), // TODO before merge: this should be configurable!
		metrics:                m,
	}
}

// enqueue sends work to be handled on the flush loop. Will return an error if the DB is panicking.
func (f *flushLoop) enqueue(request flushLoopMessage) error {
	return util.Send(f.errorMonitor, f.flushChannel, request)
}

// run is responsible for handling operations that flush data (i.e. calls to Flush() and when the mutable segment
// is sealed). In theory, this work could be done on the main control loop, but doing so would block new writes while
// a flush is in progress. In order to keep the writing threads busy, it is critical that flush do not block the
// control loop.
func (f *flushLoop) run() {
	for {
		f.metrics.SetFlushLoopPhase("idle")
		select {
		case <-f.errorMonitor.ImmediateShutdownRequired():
			f.metrics.SetFlushLoopPhase("")
			f.logger.Info("context done, shutting down disk table flush loop")
			return
		case message := <-f.flushChannel:
			if req, ok := message.(*flushLoopFlushRequest); ok {
				f.handleFlushRequest(req)
			} else if req, ok := message.(*flushLoopSealRequest); ok {
				f.handleSealRequest(req)
			} else if req, ok := message.(*flushLoopShutdownRequest); ok {
				f.metrics.SetFlushLoopPhase("")
				req.shutdownCompleteChan <- struct{}{}
				return
			} else {
				f.errorMonitor.Panic(fmt.Errorf("unknown flush message type %T", message))
				return
			}
		}
	}
}

// handleSealRequest handles the part of the seal operation that is performed on the flush loop.
// We don't want to send a flush request to a segment that has already been sealed. By performing the sealing
// on the flush loop, we ensure that this can never happen. Any previously scheduled flush requests against the
// segment that is being sealed will be processed prior to this request being processed due to the FIFO nature
// of the flush loop channel. When a sealing operation begins, the control loop blocks, and does not unblock until
// the seal is finished and a new mutable segment has been created. This means that no future flush requests will be
// sent to the segment that is being sealed, since only the control loop can schedule work for the flush loop.
func (f *flushLoop) handleSealRequest(req *flushLoopSealRequest) {

	// Flush the keymap manager.
	f.metrics.SetFlushLoopPhase("seal/keymap_flush+segment_seal")
	var keymapFlushErr error
	wg := sync.WaitGroup{}
	wg.Add(1)
	go func() {
		defer wg.Done()
		keymapFlushErr = f.keymapManager.Flush()
	}()

	// Simultaneously with flushing the keymap manager, seal the current segment.
	durableKeys, err := req.segmentToSeal.Seal(req.now)
	if err != nil {
		f.errorMonitor.Panic(fmt.Errorf("failed to seal segment %s: %w", req.segmentToSeal.String(), err))
		return
	}

	// Inform the unflushed data cache that the segment data is now durable on disk, allowing the cache
	// to asyncronously evict entries that are safe to evict.
	f.metrics.SetFlushLoopPhase("seal/report_flushed")
	err = f.unflushedDataCache.ReportFlushedSegment(durableKeys)
	if err != nil {
		f.errorMonitor.Panic(fmt.Errorf("failed to report flushed segment: %w", err))
		return
	}

	// Wait for keymap flush to complete.
	f.metrics.SetFlushLoopPhase("seal/wait_keymap_flush")
	wg.Wait()
	if keymapFlushErr != nil {
		f.errorMonitor.Panic(fmt.Errorf("failed to flush keymap: %w", keymapFlushErr))
		return
	}

	req.responseChan <- struct{}{}

	// Snapshotting can wait until after we have sent a response. No need for the Flush() caller to wait for
	// snapshotting. Flush() only cares about the data's crash durability, and is completely independent of
	// snapshotting.
	f.metrics.SetFlushLoopPhase("seal/snapshot")
	err = req.segmentToSeal.Snapshot()
	if err != nil {
		f.errorMonitor.Panic(fmt.Errorf("failed to snapshot segment %s: %w", req.segmentToSeal.String(), err))
		return
	}

	// Update the boundary file. The consumer of the snapshot uses this information to determine when segments
	// are fully copied to the snapshot directory.
	f.metrics.SetFlushLoopPhase("seal/update_boundary")
	err = f.upperBoundSnapshotFile.Update(req.segmentToSeal.SegmentIndex())
	if err != nil {
		f.errorMonitor.Panic(fmt.Errorf("failed to update upper bound snapshot file: %w", err))
	}
}

// handleFlushRequest handles the part of the flush that is performed on the flush loop.
func (f *flushLoop) handleFlushRequest(req *flushLoopFlushRequest) {
	var segmentFlushStart time.Time
	if f.metrics != nil {
		segmentFlushStart = f.clock()
	}

	// Flush the keymap manager.
	f.metrics.SetFlushLoopPhase("flush/keymap_flush+segment_flush")
	var keymapFlushErr error
	wg := sync.WaitGroup{}
	wg.Add(1)
	go func() {
		defer wg.Done()
		keymapFlushErr = f.keymapManager.Flush()
	}()

	// Simultaneously with flushing the keymap manager, flush the segment.
	flushWaitFunction, err := req.segmentToFlush.Flush()
	if err != nil {
		f.errorMonitor.Panic(fmt.Errorf("failed to flush mutable segment: %w", err))
		return
	}
	f.metrics.SetFlushLoopPhase("flush/wait_segment_flush")
	durableKeys, err := flushWaitFunction()
	if err != nil {
		f.errorMonitor.Panic(fmt.Errorf("failed to flush mutable segment: %w", err))
		return
	}
	f.metrics.SetFlushLoopPhase("flush/report_flushed")
	err = f.unflushedDataCache.ReportFlushedSegment(durableKeys) // TODO before merge: should we push reporting down into the segment?
	if err != nil {
		f.errorMonitor.Panic(fmt.Errorf("failed to report flushed segment: %w", err))
		return
	}

	// Wait for keymap flush to complete.
	f.metrics.SetFlushLoopPhase("flush/wait_keymap_flush")
	wg.Wait()
	if keymapFlushErr != nil {
		f.errorMonitor.Panic(fmt.Errorf("failed to flush keymap: %w", keymapFlushErr))
		return
	}

	if f.metrics != nil {
		segmentFlushEnd := f.clock()
		delta := segmentFlushEnd.Sub(segmentFlushStart)
		f.metrics.ReportSegmentFlushLatency(f.name, delta)
	}

	req.responseChan <- struct{}{}
}
