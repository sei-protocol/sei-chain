package flatkv

import (
	"errors"
	"fmt"

	"github.com/sei-protocol/sei-chain/sei-db/db_engine/view"
)

// This file contains the messages that can be sent to the snapshot writer's goroutine.

// snapshotRequest is a committed block offered to the writer, which decides whether it becomes a
// snapshot.
type snapshotRequest struct {
	// version is the block height this snapshot would capture.
	version int64

	// views is the block's sealed view for each database, keyed by database directory name. Each
	// carries a reservation this request owns and must hand back exactly once — a second Release on a
	// view bricks its manager.
	views map[string]view.View
}

// release hands back every reservation this request holds, so the databases can resume writing out
// later blocks. The goroutine owns this for a request it received; Offer owns it only for one it took
// reservations for but could not enqueue.
//
// Every reservation is handed back even if one of them fails, because a reservation left held stalls
// its database's flushes indefinitely. The failures are joined and returned.
func (r *snapshotRequest) release() error {
	var errs []error
	for name, sealed := range r.views {
		if relErr := sealed.Release(); relErr != nil {
			errs = append(errs,
				fmt.Errorf("release %s view at version %d: %w", name, r.version, relErr))
		}
	}
	return errors.Join(errs...)
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
