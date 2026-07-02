package wal

import "github.com/sei-protocol/sei-chain/sei-db/proto"

// A WAL for flatKV.
type FlatKVWAL interface {

	// Write a set of changes to the WAL.
	//
	// This method only schedules the write, it does not block until the write is complete.
	//
	// The FlatKVWal rejects writes for blocks if provided out of order. To avoid errors, observe
	// the following rules:
	//
	// - The block numbers passed to Write() may never decrease.
	// - If data has been written for block N, you cannot write data for block N+1 until you have called
	//   SignalEndOfBlock().
	Write(
		// The block number associated with the changeset.
		blockNumber uint64,
		// The changeset to write.
		cs []*proto.NamedChangeSet,
	) error

	// Signal that there will be no more writes for the current block number. Attempting to write additional
	// changes for the same block number after calling this method may result in an error.
	//
	// Similar to Write(), this method is asynchronous. Calling this method does not, by itself, make data immediately
	// crash durable.
	SignalEndOfBlock() error

	// Flush the WAL to disk. All data previously passed to Write() before this call will be crash durable
	// after this call returns.
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

	// Create an iterator over the WAL, starting at the given block number. Iterates all data passed to Write()
	// before this call. Data written after this call is not iterated.
	Iterator(startingBlockNumber uint64) (FlatKVWalIterator, error)

	// Close the WAL, flushing any pending writes and releasing resources.
	Close() error
}

// Iterates over data in a flatKV WAL, in ascending block order, yielding one entry per block. All changesets
// written for a block (across one or more Write calls) are coalesced, in write order, into that block's single
// entry; the entry's EndOfBlock field is always false. Incomplete trailing blocks (those with no end-of-block
// marker) are not yielded.
type FlatKVWalIterator interface {
	// Next advances the iterator to the next block. It returns false when iteration is complete (no more
	// blocks), and returns an error if advancing failed. After Next returns (false, nil), iteration is
	// complete; after it returns an error, the iterator must not be used further (other than Close).
	Next() (bool, error)

	// Entry returns the coalesced entry for the block at the iterator's current position. It is only valid to
	// call Entry after Next has returned (true, nil). The returned entry must not be modified.
	Entry() *FlatKVWalEntry

	// Close releases the resources held by the iterator.
	Close() error
}
