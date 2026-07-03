package disktable

import (
	"github.com/sei-protocol/sei-chain/sei-db/db_engine/litt/disktable/segment"
	"github.com/sei-protocol/sei-chain/sei-db/db_engine/litt/types"
)

// This file contains various messages that can be sent to the disk table's control loop.

// controlLoopMessage is an interface for messages sent to the control loop via controlLoop.enqueue.
type controlLoopMessage interface {
	// If this is an empty interface, then the golang type system will not complain if non-implementing types are
	// passed to the control loop.
	unimplemented()
}

// controlLoopFlushRequest is a request to flush the writer that is sent to the control loop.
type controlLoopFlushRequest struct {
	controlLoopMessage

	// responseChan produces a value when the flush is complete.
	responseChan chan struct{}
}

// controlLoopWriteRequest is a request to write a key-value pair that is sent to the control loop.
type controlLoopWriteRequest struct {
	controlLoopMessage

	// values is a slice of key-value pairs to write.
	values []*types.PutRequest
}

// controlLoopSetShardingFactorRequest is a request to set the sharding factor that is sent to the control loop.
type controlLoopSetShardingFactorRequest struct {
	controlLoopMessage

	// shardingFactor is the new sharding factor to set.
	shardingFactor uint8
}

// controlLoopShutdownRequest is a request to shut down the table that is sent to the control loop.
type controlLoopShutdownRequest struct {
	controlLoopMessage

	// responseChan will produce a single struct{} when the control loop has stopped
	// (i.e. when the handleShutdownRequest is complete).
	shutdownCompleteChan chan struct{}
}

// controlLoopGCRequest is a request for the control loop to delete the files of segments whose keymap entries the
// keymap manager has already durably deleted. Choosing which segments to collect (and scheduling those keymap
// deletes) happens on the GC manager; RunGC drives the two separately.
type controlLoopGCRequest struct {
	controlLoopMessage

	// completionChan produces a value when the file deletion is complete.
	completionChan chan struct{}
}

// controlLoopOpenIteratorRequest is a request to open an iterator that is sent to the control loop. The
// control loop seals the current mutable segment (if it has any keys) so that the full set of keys in
// scope is readable, reserves each snapshot segment (so its files survive until the iterator closes even
// if GC collects it meanwhile), bumps the open-iterator count (used only for the open-iterator metric, not
// to gate GC), and returns the ordered snapshot of sealed segments the iterator will walk.
type controlLoopOpenIteratorRequest struct {
	controlLoopMessage

	// responseChan produces the ordered (lowest-to-highest index) snapshot of sealed segments in scope.
	responseChan chan []*segment.Segment
}

// controlLoopCloseIteratorRequest is a request to close an iterator that is sent to the control loop. The
// iterator releases its own segment reservations on Close; this only decrements the open-iterator count for
// the metric (GC is not gated on open iterators).
type controlLoopCloseIteratorRequest struct {
	controlLoopMessage

	// completionChan produces a value when the close has been processed.
	completionChan chan struct{}
}

// controlLoopBoundaryKeysRequest is a request for the oldest and newest primary keys that is sent to the
// control loop.
type controlLoopBoundaryKeysRequest struct {
	controlLoopMessage

	// responseChan produces the boundary keys response.
	responseChan chan *boundaryKeysResponse
}

// boundaryKeysResponse carries the oldest and newest primary keys back from the control loop.
type boundaryKeysResponse struct {
	// oldestKey is the oldest non-deleted primary key, valid only if oldestExists is true.
	oldestKey []byte
	// oldestExists is false if the table contains no keys.
	oldestExists bool
	// newestKey is the newest primary key, valid only if newestExists is true.
	newestKey []byte
	// newestExists is false if the table contains no keys.
	newestExists bool
	// err is non-nil if the boundary keys could not be computed.
	err error
}
