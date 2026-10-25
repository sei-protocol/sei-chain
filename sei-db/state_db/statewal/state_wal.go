package statewal

import (
	"context"

	controllertypes "github.com/sei-protocol/sei-chain/sei-db/controller/types"

	"github.com/sei-protocol/sei-chain/sei-db/proto"
	"github.com/sei-protocol/sei-chain/sei-db/seiwal"
	"github.com/sei-protocol/sei-chain/sei-db/state_db/giga/types/mutation"
)

// A WAL for state. Each block is written as mutations and read back as changesets.
//
// A StateWAL is not safe for concurrent use. Callers must serialize their calls to a single instance;
// in particular Write maintains write-ordering state that is not internally locked.
//
// The changesets returned by the iterator are owned by the WAL and must not be modified.
type StateWAL interface {
	controllertypes.PrunableStore

	// Write a block's mutations to the WAL as a single record.
	//
	// This method only schedules the write, it does not block until the write is complete. It does block while the
	// WAL is behind by more than Config.RequestBufferSize blocks.
	//
	// Write takes ownership of mutations. Once the call begins, neither mutations nor any byte slice reachable
	// through it may be read or modified.
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

	// Flush the WAL to disk, making every block written so far crash durable, and wait until every listener has
	// returned for each of those blocks.
	Flush() error

	// Register a listener that receives the hash of each block written, one call per block, in block order, with
	// no gaps. Listeners are meant for steady state, after setup; during setup, read the hashes of specific blocks
	// with Iterator() and GetHash().
	//
	// Listeners run on a goroutine owned by the WAL, so a listener that falls behind eventually blocks Write(). A
	// listener must not call back into the WAL: RegisterHashListener(), Write(), Flush(), and Close() wait on that
	// goroutine and would deadlock, and the other methods are not safe to call concurrently with the WAL's owner.
	// An error from a listener bricks the WAL, and no listener receives a hash for any later block.
	RegisterHashListener(
		// The listener to register, or nil to only read the most recent hash.
		listener HashListener,
	) (
		// If false, no block has a hash yet: the WAL was opened empty and no hash has been delivered since.
		// blockNumber and hash are then undefined, and the listener's first delivery is the first block
		// written.
		ok bool,
		// The block of the most recent hash delivered to listeners, or of the last stored block if none has
		// been delivered since the WAL was opened. The listener's first delivery is for blockNumber+1.
		blockNumber uint64,
		// The hash of the key-value pairs modified by executing the transactions in block blockNumber.
		hash [32]byte,
		// Any error encountered while registering.
		err error,
	)

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

// A callback that receives the hash of the key-value pairs modified by executing the transactions in a block. Its
// signature matches AppHashBuilder.ReportBUDHash(), so that method registers as one directly.
//
// The hash is currently a stopgap placeholder, not a BUD as the BUD spec defines it; see computePlaceholderHash().
type HashListener func(
	// Cancelled when the StateWAL stops. A listener blocked when it is cancelled must return.
	ctx context.Context,
	// The block the hash belongs to.
	blockHeight uint64,
	// The hash of the key-value pairs modified by executing the transactions in the block.
	hash [32]byte,
) error

// An iterator over the blocks of a StateWAL. Entry() returns (blockNumber, changesets).
type StateWALIterator interface {
	seiwal.Iterator[[]*proto.NamedChangeSet]

	// GetHash returns the hash delivered to listeners for the block at the iterator's current position. It is valid
	// only after Next() returns true.
	GetHash() ([32]byte, error)
}
