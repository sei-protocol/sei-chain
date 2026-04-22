package disktable

import (
	"time"

	"github.com/Layr-Labs/eigenda/litt/disktable/segment"
)

// FlushLoopMessage is an interface for messages sent to the flush loop via flushLoop.enqueue.
type flushLoopMessage interface {
	// unimplemented is a no-op function that is used to satisfy the interface.
	unimplemented()
}

// flushLoopFlushRequest is a request to flush the writer that is sent to the flush loop.
type flushLoopFlushRequest struct {
	flushLoopMessage

	// flushWaitFunction is the function that will wait for the flush to complete.
	flushWaitFunction segment.FlushWaitFunction

	// responseChan sends an object when the flush is complete.
	responseChan chan struct{}
}

// flushLoopSealRequest is a request to seal the mutable segment that is sent to the flush loop.
type flushLoopSealRequest struct {
	flushLoopMessage

	// the time when the segment is sealed
	now time.Time
	// segmentToSeal is the segment that is being sealed.
	segmentToSeal *segment.Segment
	// responseChan sends an object when the seal is complete.
	responseChan chan struct{}
}

// flushLoopShutdownRequest is a request to shut down the flush loop.
type flushLoopShutdownRequest struct {
	flushLoopMessage

	// responseChan will produce a single struct{} when the flush loop has stopped.
	shutdownCompleteChan chan struct{}
}
