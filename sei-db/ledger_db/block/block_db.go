package block

import (
	"context"
	"errors"
	"time"
)

// ErrNoBlocks is returned by GetLowestBlockHeight and GetHighestBlockHeight
// when the database contains no blocks.
var ErrNoBlocks = errors.New("block db: no blocks")

// Transaction is the BlockDB's view of a single transaction inside a block:
// its hash plus its raw bytes. Implementations are expected to be cheap
// (typically just a struct returning pre-computed fields).
type Transaction interface {
	// Hash returns the canonical transaction hash used for indexing.
	Hash() []byte
	// Bytes returns the raw, on-the-wire transaction bytes.
	Bytes() []byte
}

// Block is the BlockDB's view of a finalized block. The interface intentionally
// exposes only what BlockDB itself needs to index and serve reads — backends
// must not assume any particular concrete implementation. Methods returning
// slices may allocate; callers that index repeatedly should cache the result.
type Block interface {
	// Hash returns the canonical block hash used for indexing.
	Hash() []byte
	// Height returns the block height (used as the key for the height index).
	Height() uint64
	// Time returns the block timestamp.
	Time() time.Time
	// Transactions returns the block's transactions in order.
	Transactions() []Transaction
}

// A database for storing finalized block and transaction data.
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
	WriteBlock(ctx context.Context, block Block) error

	// Blocks until all pending writes are flushed to disk. Any call to WriteBlock issued before calling Flush()
	// will be crash-durable after Flush() returns. Calls to WriteBlock() made concurrently with Flush() may or
	// may not be crash-durable after Flush() returns (but are otherwise eventually durable).
	//
	// It is not required to call Flush() in order to ensure data is written to disk. The database asyncronously
	// pushes data down to disk even if Flush() is never called. Flush() just allows you to syncronize an external
	// goroutine with the database's internal write loop.
	Flush(ctx context.Context) error

	// Retrieves a block by its hash.
	GetBlockByHash(ctx context.Context, hash []byte) (block Block, ok bool, err error)

	// Retrieves a block by its height.
	GetBlockByHeight(ctx context.Context, height uint64) (block Block, ok bool, err error)

	// Retrieves a transaction by its hash.
	GetTransactionByHash(ctx context.Context, hash []byte) (transaction Transaction, ok bool, err error)

	// Schedules pruning for all blocks with a height less than the given height. Pruning is asynchronous,
	// and so this method does not provide any guarantees about when the pruning will complete. It is possible
	// that some data will not be pruned if the database is closed before the pruning is scheduled.
	Prune(ctx context.Context, lowestHeightToKeep uint64) error

	// Retrieves the lowest block height in the database.
	GetLowestBlockHeight(ctx context.Context) (uint64, error)

	// Retrieves the highest block height in the database.
	GetHighestBlockHeight(ctx context.Context) (uint64, error)

	// Closes the database and releases any resources. Any in-flight writes are fully flushed to disk before this
	// method returns.
	Close(ctx context.Context) error
}
