package disktable

import (
	"fmt"
	"time"

	"github.com/Layr-Labs/eigenda/litt/metrics"
	"github.com/Layr-Labs/eigenda/litt/util"
	"github.com/Layr-Labs/eigensdk-go/logging"
)

// flushLoop is a struct that runs a goroutine that is responsible for blocking on flush operations.
type flushLoop struct {
	logger logging.Logger

	// the parent disk table
	diskTable *DiskTable

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
		select {
		case <-f.errorMonitor.ImmediateShutdownRequired():
			f.logger.Infof("context done, shutting down disk table flush loop")
			return
		case message := <-f.flushChannel:
			if req, ok := message.(*flushLoopFlushRequest); ok {
				f.handleFlushRequest(req)
			} else if req, ok := message.(*flushLoopSealRequest); ok {
				f.handleSealRequest(req)
			} else if req, ok := message.(*flushLoopShutdownRequest); ok {
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
	durableKeys, err := req.segmentToSeal.Seal(req.now)
	if err != nil {
		f.errorMonitor.Panic(fmt.Errorf("failed to seal segment %s: %w", req.segmentToSeal.String(), err))
		return
	}

	// Flush the keys that are now durable in the segment.
	err = f.diskTable.writeKeysToKeymap(durableKeys)
	if err != nil {
		f.errorMonitor.Panic(fmt.Errorf("failed to flush keys: %w", err))
		return
	}

	req.responseChan <- struct{}{}

	// Snapshotting can wait until after we have sent a response. No need for the Flush() caller to wait for
	// snapshotting. Flush() only cares about the data's crash durability, and is completely independent of
	// snapshotting.
	err = req.segmentToSeal.Snapshot()
	if err != nil {
		f.errorMonitor.Panic(fmt.Errorf("failed to snapshot segment %s: %w", req.segmentToSeal.String(), err))
		return
	}

	// Update the boundary file. The consumer of the snapshot uses this information to determine when segments
	// are fully copied to the snapshot directory.
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

	durableKeys, err := req.flushWaitFunction()
	if err != nil {
		f.errorMonitor.Panic(fmt.Errorf("failed to flush mutable segment: %w", err))
		return
	}

	if f.metrics != nil {
		segmentFlushEnd := f.clock()
		delta := segmentFlushEnd.Sub(segmentFlushStart)
		f.metrics.ReportSegmentFlushLatency(f.name, delta)
	}

	err = f.diskTable.writeKeysToKeymap(durableKeys)
	if err != nil {
		f.errorMonitor.Panic(fmt.Errorf("failed to flush keys: %w", err))
		return
	}

	req.responseChan <- struct{}{}
}
