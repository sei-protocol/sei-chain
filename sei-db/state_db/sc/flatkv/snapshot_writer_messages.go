package flatkv

import "fmt"

// This file contains the messages that can be sent to the snapshot writer's goroutine.

// snapshotRequest is a committed block offered to the writer, which decides whether it becomes a
// snapshot.
type snapshotRequest struct {
	// blockView is the view of every database at the height this snapshot would capture. It carries a
	// reservation this request owns and must hand back exactly once — a second Release() on a view bricks
	// its manager.
	blockView *storeView
}

// release() hands back the reservations this request holds, so the databases can resume writing out
// later blocks. The goroutine owns this for a request it received; Offer() owns it only for one it took
// reservations for but could not enqueue.
func (r *snapshotRequest) release() error {
	if err := r.blockView.release(); err != nil {
		return fmt.Errorf("release views at version %d: %w", r.blockView.blockHeight, err)
	}
	return nil
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
