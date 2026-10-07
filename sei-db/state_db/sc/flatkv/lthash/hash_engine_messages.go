package lthash

import (
	"fmt"

	"github.com/sei-protocol/sei-chain/sei-db/state_db/sc/flatkv/sview"
)

// The messages the phases send one another, and the state one block carries as it moves between them.

// hashRequest is one sealed block for the engine to hash.
type hashRequest struct {
	// blockNumber is the height being hashed.
	blockNumber int64

	// current is the block's own sealed view. The gatherer reads this block's mutations from it.
	current *sview.StoreView
}

// reserve takes this request's own reservation on the block's view, so that it cannot be torn down while
// the engine still has to read it.
func (r *hashRequest) reserve() error {
	if err := r.current.Reserve(); err != nil {
		return fmt.Errorf("reserve block %d: %w", r.blockNumber, err)
	}
	return nil
}

// Releases the reservation this request owns, so the databases can resume flushing.
func (r *hashRequest) release() error {
	if err := r.current.Release(); err != nil {
		return fmt.Errorf("release block %d: %w", r.blockNumber, err)
	}
	return nil
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

// chunkResult is one chunk of one block, folded per module.
type chunkResult struct {
	// The database the chunk belongs to.
	dbName string

	// The chunk's delta for each module it touched.
	modules []moduleDelta

	// Set when a key in the chunk names no module, in which case modules is nil.
	err error
}

// moduleDelta is one module's share of a chunk.
type moduleDelta struct {
	module string
	info   *ModuleHashInfo
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
