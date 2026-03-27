package blockdb

import "context"

// A binary transaction with its hash.
type BinaryTransaction struct {
	// The hash of the transaction.
	Hash []byte
	// The binary transaction data.
	Transaction []byte
}

// A binary block with its transactions and hash.
type BinaryBlock struct {
	// The height of the block. Must be unique.
	Height uint64
	// The hash of the block. Must be unique.
	Hash []byte
	// The binary block data, not including transaction data (unless you are ok with wasting space)
	BlockData []byte
	// The transactions in the block and their hashes.
	Transactions []*BinaryTransaction
}

// A database for storing binary block and transaction data.
//
// This store is fully threadsafe. All writes are atomic (that is, after a crash you will either see the write or
// you will not see it at all, i.e. partial writes are not possible). Multiple writes are not atomic with respect
// to each other, meaning if you write A then B and crash, you may observe B but not A (only possible when sharding
// is enabled). Within a single session, read-your-writes consistency is provided.
type BlockDB interface {

	// Write a block to the database.
	//
	// This method may return immediately and does not necessarily wait for the block to be written to disk.
	// Call Flush() if you need to wait until the block is written to disk.
	WriteBlock(ctx context.Context, block *BinaryBlock) error

	// Blocks until all pending writes are flushed to disk. Any call to WriteBlock issued before calling Flush()
	// will be crash-durable after Flush() returns. Calls to WriteBlock() made concurrently with Flush() may or
	// may not be crash-durable after Flush() returns (but are otherwise eventually durable).
	//
	// It is not required to call Flush() in order to ensure data is written to disk. The database asyncronously
	// pushes data down to disk even if Flush() is never called. Flush() just allows you to syncronize an external
	// goroutine with the database's internal write loop.
	Flush(ctx context.Context) error

	// Retrieves a block by its hash.
	GetBlockByHash(ctx context.Context, hash []byte) (block *BinaryBlock, ok bool, err error)

	// Retrieves a block by its height.
	GetBlockByHeight(ctx context.Context, height uint64) (block *BinaryBlock, ok bool, err error)

	// Retrieves a transaction by its hash.
	GetTransactionByHash(ctx context.Context, hash []byte) (transaction *BinaryTransaction, ok bool, err error)

	// Schedules pruning for all blocks with a height less than the given height. Pruning is asyncronous,
	// and so this method does not provide any guarantees about when the pruning will complete. It is possible
	// that some data will not be pruned if the database is closed before the pruning is scheduled.
	Prune(ctx context.Context, lowestHeightToKeep uint64) error

	// Closes the database and releases any resources. Any in-flight writes are fully flushed to disk before this
	// method returns.
	Close(ctx context.Context) error
}
