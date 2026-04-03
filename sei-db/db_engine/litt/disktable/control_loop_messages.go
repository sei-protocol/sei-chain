package disktable

import "github.com/Layr-Labs/eigenda/litt/types"

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
	values []*types.KVPair
}

// controlLoopSetShardingFactorRequest is a request to set the sharding factor that is sent to the control loop.
type controlLoopSetShardingFactorRequest struct {
	controlLoopMessage

	// shardingFactor is the new sharding factor to set.
	shardingFactor uint32
}

// controlLoopShutdownRequest is a request to shut down the table that is sent to the control loop.
type controlLoopShutdownRequest struct {
	controlLoopMessage

	// responseChan will produce a single struct{} when the control loop has stopped
	// (i.e. when the handleShutdownRequest is complete).
	shutdownCompleteChan chan struct{}
}

// controlLoopGCRequest is a request to run garbage collection that is sent to the control loop.
type controlLoopGCRequest struct {
	controlLoopMessage

	// completionChan produces a value when the garbage collection is complete.
	completionChan chan struct{}
}
