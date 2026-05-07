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

// Transaction is the BlockDB's view of a transaction's *body* — what's
// invariant across every block that includes it. Per-block-occurrence data
// (height, index, execution result) lives on Result, returned alongside the
// Transaction by GetTransactionByHash.
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

// Result is the BlockDB's view of one transaction's post-execution outcome
// in a specific block: the marshaled execution result plus where it landed
// (block height + position in that block). Used both as the input to
// SetTransactionResults and as the per-occurrence value returned by
// GetTransactionByHash.
//
// The interface stays chain-agnostic — callers wrap their concrete result
// types (e.g. abci.ExecTxResult) in a small adapter. Bytes() is permitted
// to return the wire encoding lazily; backends that need to copy/index
// will call it during SetTransactionResults under the assumption it is
// inexpensive (typically a single proto Marshal).
type Result interface {
	// Bytes returns the marshaled execution result for one transaction.
	Bytes() []byte
	// Height returns the block height of the block that produced this result.
	Height() uint64
	// Index returns the position of the transaction within that block.
	Index() uint32
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
	// Transactions(); each entry corresponds positionally to the transaction at that index,
	// and its Height()/Index() must match the block's height and the position in this slice.
	//
	// Returns ErrUnknownBlock if no block with the given hash has been written, and
	// ErrResultCountMismatch if len(results) does not match the block's transaction count.
	//
	// Calling SetTransactionResults a second time for the same block hash overwrites the
	// previously attached results.
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

	// GetTransactionByHash returns the canonical transaction body and the list
	// of recorded executions for that hash. Because the same tx body can be
	// included in multiple blocks (different lanes producing the same tx), the
	// API surfaces every recorded execution; the caller picks which is canonical
	// for its purposes (e.g. preferring a successful execution).
	//
	// Returns:
	//   found=false                          unknown tx hash; tx and results are nil/empty.
	//   found=true,  len(results)==0         tx exists in some block but no execution results
	//                                        have been attached yet (between WriteBlock and
	//                                        SetTransactionResults).
	//   found=true,  len(results)>=1         one entry per block that has had results attached;
	//                                        order is unspecified.
	//
	// The returned Transaction's Hash and Bytes are the same regardless of
	// which block included it (cryptographic hash collision aside).
	GetTransactionByHash(ctx context.Context, hash []byte) (tx Transaction, results []Result, found bool, err error)

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
