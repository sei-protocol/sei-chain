package block

import (
	"context"
	"errors"
	"time"
)

// ErrNoBlocks is returned by GetLowestBlockHeight and GetHighestBlockHeight
// when the database contains no blocks.
var ErrNoBlocks = errors.New("block db: no blocks")

// ErrUnknownBlock is returned by SetTransactionResults when the referenced
// block hash has not been written to the database.
var ErrUnknownBlock = errors.New("block db: unknown block hash")

// ErrResultCountMismatch is returned by SetTransactionResults when the supplied
// results slice doesn't match the number of transactions in the referenced block.
var ErrResultCountMismatch = errors.New("block db: result count does not match transaction count")

// Transaction is the BlockDB's view of a single transaction inside a block:
// its hash, raw bytes, post-execution result, plus its position within the
// chain (height + index). Result returns ok=false until SetTransactionResults
// has been called for the parent block.
type Transaction interface {
	// Hash returns the canonical transaction hash used for indexing.
	Hash() []byte
	// Bytes returns the raw, on-the-wire transaction bytes.
	Bytes() []byte
	// Result returns the marshaled execution result and ok=true once it has
	// been attached via SetTransactionResults. Returns (nil, false) for
	// transactions whose parent block has been written but not yet had
	// results recorded — callers should treat that as "not yet executed".
	Result() (bytes []byte, ok bool)
	// Height returns the height of the block this transaction belongs to.
	Height() uint64
	// Index returns the position of this transaction within its block.
	Index() uint32
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
	// Transactions returns the block's transactions in order. Each Transaction
	// must report Height() == this block's height and Index() == its position
	// in the slice. Result() may be empty at WriteBlock time; results are
	// supplied separately via SetTransactionResults.
	Transactions() []Transaction
}

// Result is the BlockDB's view of one transaction's post-execution result,
// supplied to SetTransactionResults after the application has executed the
// block. The interface keeps BlockDB chain-agnostic — callers wrap their
// concrete result types (e.g. abci.ExecTxResult) in a small adapter that
// returns the marshaled bytes.
type Result interface {
	// Bytes returns the marshaled execution result for one transaction.
	Bytes() []byte
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

	// SetTransactionResults attaches per-transaction execution results to a previously written
	// block, identified by its block hash. results must be the same length as the block's
	// Transactions(); each entry corresponds positionally to the transaction at that index.
	//
	// Returns ErrUnknownBlock if no block with the given hash has been written, and
	// ErrResultCountMismatch if len(results) does not match the block's transaction count.
	//
	// Like WriteBlock, this is async with respect to disk persistence; pair with Flush()
	// for crash durability.
	SetTransactionResults(ctx context.Context, blockHash []byte, results []Result) error

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
