package statewal

import (
	"github.com/sei-protocol/sei-chain/sei-db/controller"
	"github.com/sei-protocol/sei-chain/sei-db/proto"
	"github.com/sei-protocol/sei-chain/sei-db/seiwal"
)

// A WAL for state.
//
// A StateWAL is not safe for concurrent use. Callers must serialize their calls to a single instance;
// in particular Write maintains write-ordering state that is not internally locked.
//
// Slices are not copied at the call boundary. Changesets passed to Write — and every byte slice reachable
// through them — must not be modified after the call: the WAL retains them and serializes them
// asynchronously, so mutating them races the WAL and can corrupt what is persisted. Likewise the changesets
// returned by the iterator are owned by the WAL and must be treated as read-only. Callers that need to
// mutate such data must copy it first.
type StateWAL interface {
	controller.PrunableStore

	// Write a block's changes to the WAL as a single record.
	//
	// This method only schedules the write, it does not block until the write is complete.
	//
	// cs, and every byte slice reachable through it (changeset keys and values), must not be modified after
	// this call. Callers that need to modify those buffers must copy them first.
	//
	// A nil entry in cs is rejected synchronously with an error and leaves the WAL usable; cs itself may be
	// nil or empty.
	//
	// Blocks must be written in contiguous ascending order, one Write per block. The first block written to
	// an empty WAL may be any number; every block after it must be exactly one greater than the last, so
	// repeats and forward jumps are rejected. This survives close and reopen.
	//
	// Violations are reported synchronously and leave the WAL usable.
	Write(
		// The block number associated with the changeset.
		blockNumber uint64,
		// The changeset to write.
		cs []*proto.NamedChangeSet,
	) error

	// Flush the WAL to disk, making every block written so far crash durable.
	Flush() error

	// Get the range of block numbers stored in the WAL.
	GetStoredRange() (
		// If true, there is data in the WAL. If false, the WAL is empty and startBlockNumber and
		// endBlockNumber are undefined.
		ok bool,
		// The lowest block number stored in the WAL, inclusive. Only valid if ok is true.
		startBlockNumber uint64,
		// The highest block number stored in the WAL, inclusive. Only valid if ok is true.
		endBlockNumber uint64,
		// Any error encountered while retrieving the range.
		err error,
	)

	// Prune the WAL, removing all entries with block numbers less than lowestBlockNumberToKeep.
	//
	// This method merely schedules the prune operation, it does not block until the prune is complete. Pruning
	// is async and lazy, and implementations are free to delay pruning arbitrarily long. If crashed or closed
	// before the prune is complete, the WAL may not attempt to prune again on the next open unless Prune() is
	// called again or for a higher block number.
	Prune(lowestBlockNumberToKeep uint64) error

	// Create an iterator over the WAL across the inclusive block range [startingBlockNumber, endingBlockNumber].
	//
	// The iterator yields no block below startingBlockNumber or above endingBlockNumber. It is an error for
	// endingBlockNumber to be below startingBlockNumber, or for endingBlockNumber to be above the highest block
	// number currently stored in the WAL (including when the WAL is empty); both are reported as
	// seiwal.ErrIteratorRange and leave the WAL usable.
	//
	// The iterator reads a consistent, point-in-time snapshot of the WAL taken at some instant between the
	// start and the return of this call. Data written before that instant is included; data written after it
	// is not. For data written concurrently with this call, whether it is included is unspecified.
	//
	// The iterator yields one entry per block in ascending block order. Its Entry() returns (blockNumber,
	// changesets), where changesets are the changes written for that block. The returned changesets, and
	// every byte slice reachable through them, must be treated as read-only.
	Iterator(startingBlockNumber uint64, endingBlockNumber uint64) (seiwal.Iterator[[]*proto.NamedChangeSet], error)

	// Close the WAL, flushing the blocks written so far to disk and releasing resources.
	Close() error
}
