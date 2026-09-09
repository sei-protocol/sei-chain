package flatkv

import (
	"fmt"

	"github.com/sei-protocol/sei-chain/sei-db/state_db/sc/flatkv/sview"
)

// This file contains the messages that can be sent to the snapshot writer's goroutine.

// snapshotRequest is a committed block offered to the writer, which decides whether it becomes a
// snapshot.
type snapshotRequest struct {
	// blockView is the view of every database at the height this snapshot would capture. It carries a
	// reservation this request owns and must release exactly once — a second Release() on a view bricks
	// its manager.
	blockView *sview.StoreView
}

// Releases the reservations this request holds, so the databases can resume writing out later
// blocks. The goroutine owns this for a request it received; Offer() owns it only for one it took
// reservations for but could not enqueue.
func (r *snapshotRequest) release() error {
	if err := r.blockView.Release(); err != nil {
		return fmt.Errorf("release views at version %d: %w", r.blockView.BlockHeight(), err)
	}
	return nil
}

// cloneRequest asks the writer to materialize a snapshot into a destination directory.
type cloneRequest struct {
	// targetVersion is the height to clone at or below. 0 names the active snapshot.
	targetVersion int64

	// destDir is the directory the snapshot is materialized into.
	destDir string

	// responseChan produces the outcome of the clone. Buffered, so the writer answering it cannot
	// block on a caller that has already given up.
	responseChan chan error
}

// newCloneRequest describes a clone of the snapshot at or below targetVersion into destDir.
func newCloneRequest(targetVersion int64, destDir string) *cloneRequest {
	return &cloneRequest{
		targetVersion: targetVersion,
		destDir:       destDir,
		responseChan:  make(chan error, 1),
	}
}

// flushRequest asks the writer to report once it has dealt with everything enqueued ahead of it.
type flushRequest struct {
	// responseChan produces a value once every message enqueued ahead of this one has been dealt with.
	// Buffered, so the writer answering it cannot block on a caller that has already given up.
	responseChan chan struct{}
}

// newFlushRequest describes a wait for the writer to catch up.
func newFlushRequest() *flushRequest {
	return &flushRequest{responseChan: make(chan struct{}, 1)}
}
