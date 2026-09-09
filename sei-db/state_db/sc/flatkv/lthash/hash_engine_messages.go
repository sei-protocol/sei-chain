package lthash

import (
	"errors"
	"fmt"

	"github.com/sei-protocol/sei-chain/sei-db/state_db/sc/flatkv/sview"
)

// The messages the phases send one another, and the state one block carries as it moves between them.

// hashRequest is one sealed block for the engine to hash.
type hashRequest struct {
	// blockNumber is the height being hashed.
	blockNumber int64

	// current is the block's own sealed view. The gatherer reads this block's diff from it.
	current *sview.StoreView

	// previous is the preceding block's view. The lattice hash is a delta, so every changed key's prior
	// value is read here — and holding this reservation is what keeps the databases at the preceding
	// version while that read happens. Releasing it early yields a wrong hash, silently.
	previous *sview.StoreView
}

// reserve takes this request's own reservation on both views, so that neither can be torn down while the
// engine still has to read it. On failure it releases whatever it took.
func (r *hashRequest) reserve() error {
	if err := r.current.Reserve(); err != nil {
		return fmt.Errorf("reserve block %d: %w", r.blockNumber, err)
	}
	if err := r.previous.Reserve(); err != nil {
		return errors.Join(
			fmt.Errorf("reserve block %d's predecessor: %w", r.blockNumber, err),
			r.current.Release())
	}
	return nil
}

// Releases the reservations this request owns, so the databases can resume flushing.
//
// Both are released even if one fails, because a reservation left held stalls its database's flushes
// indefinitely.
func (r *hashRequest) release() error {
	currentErr := r.current.Release()
	previousErr := r.previous.Release()
	if currentErr != nil {
		return currentErr
	}
	return previousErr
}

// gatheredBlock is one block the gather phase has finished with, waiting to be folded onto the
// running hash.
type gatheredBlock struct {
	// blockNumber is the height this job hashes.
	blockNumber int64

	// hashes is this block's leaf hashing in flight, which the combiner drains to completion.
	hashes leafHashes

	// err is set when the gatherer could not produce this block's chunks at all, in which case hashes is
	// zero and the combiner fails the block rather than reading results.
	err error
}

// chunkResult is one chunk of one block, folded.
type chunkResult struct {
	key  ModuleKey
	info *ModuleHashInfo
}

// flushRequest asks the engine to report once it has dealt with everything queued ahead of it.
type flushRequest struct {
	// done is closed once every message queued ahead of this one has been dealt with. A channel rather
	// than a value, so the engine answering it can never block on a caller that has given up.
	doneChan chan struct{}
}

func newFlushRequest() *flushRequest {
	return &flushRequest{doneChan: make(chan struct{})}
}
