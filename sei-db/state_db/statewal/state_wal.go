package statewal

import (
	controllertypes "github.com/sei-protocol/sei-chain/sei-db/controller/types"

	"github.com/sei-protocol/sei-chain/sei-db/seiwal"
	"github.com/sei-protocol/sei-chain/sei-db/state_db/giga/types/mutation"
)

// A WAL for state. Each block is written and read back as mutations.
//
// A StateWAL is not safe for concurrent use. Callers must serialize their calls to a single instance;
// in particular Write maintains write-ordering state that is not internally locked.
//
// The mutations returned by the iterator own their bytes, which may be retained and must not be modified.
type StateWAL interface {
	controllertypes.PrunableStore

	// Write a block's mutations to the WAL as a single record.
	//
	// This method only schedules the write, it does not block until the write is complete. It does block while the
	// WAL is behind by more than Config.RequestBufferSize blocks.
	//
	// Write retains mutations and the byte slices reachable through them, none of which may be modified after the
	// call. The block is stored with its mutations in the order given.
	//
	// Blocks must be written in contiguous ascending order, one Write per block. The first block written to
	// an empty WAL may be any number; every block after it must be exactly one greater than the last, so
	// repeats and forward jumps are rejected. This survives close and reopen.
	//
	// Violations are reported synchronously and leave the WAL usable.
	Write(
		// The block number associated with the mutations.
		blockNumber uint64,
		// The block's mutations, one per key the block writes.
		mutations []mutation.Mutation,
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
	// The iterator yields one entry per block in ascending block order.
	Iterator(startingBlockNumber uint64, endingBlockNumber uint64) (StateWALIterator, error)

	// Close blocks until all work submitted before the call has been fully handled, then releases the WAL's
	// resources.
	Close() error
}

// An iterator over the blocks of a StateWAL. Entry() returns (blockNumber, mutations). The mutations own their
// bytes, so they may be retained past the iterator's next position, but must not be modified.
type StateWALIterator interface {
	seiwal.Iterator[[]mutation.Mutation]
}
