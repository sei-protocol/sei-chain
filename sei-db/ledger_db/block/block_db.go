package block

import (
	"context"
	"errors"
	"time"
)

// ErrNoBlocks is returned by GetLowestBlockHeight and GetHighestBlockHeight
// when the database contains no blocks.
var ErrNoBlocks = errors.New("block db: no blocks")

// Transaction is the BlockDB's view of a transaction inside a block: its
// hash plus its raw bytes. BlockDB itself is block-storage-only — it does
// not index transactions by hash. Per the canonical-receipt-lookup design,
// tx-by-hash routing belongs in a separate Receipt Store; BlockDB exposes
// per-tx Hash() so a Receipt Store (or any other caller) can iterate
// `Block.Transactions()` and register its own (txHash → block, index)
// mapping at WriteBlock time.
type Transaction interface {
	// Hash returns the canonical transaction hash.
	Hash() []byte
	// Bytes returns the raw, on-the-wire transaction bytes.
	Bytes() []byte
}

// Block is the BlockDB's view of a finalized block. The interface intentionally
// exposes only what BlockDB itself needs to index and serve reads — backends
// must not assume any particular concrete implementation.
//
// Backends are permitted to call Transactions() multiple times across the
// block's lifetime in storage. Implementations that pay a non-trivial cost
// per call (allocation, hashing) should memoize the result at construction.
type Block interface {
	// Hash returns the canonical block hash used for indexing.
	Hash() []byte
	// Height returns the block height (used as the key for the height index).
	Height() uint64
	// Time returns the block timestamp.
	Time() time.Time
	// Transactions returns the block's transactions in order. Must be cheap
	// to call repeatedly — backends may call it more than once per block.
	Transactions() []Transaction
}

// A database for storing finalized blocks. Block-only — the canonical
// "transaction by hash → execution result" lookup belongs in a separate
// Receipt Store (see the Giga Transaction Query proposal); a future
// Receipt Store reads tx bodies out of BlockDB by (blockHash, index)
// once it has resolved a hash.
//
// This store is fully threadsafe. All writes are atomic (after a crash
// you will either see the write or you will not see it at all, i.e.
// partial writes are not possible). Multiple writes are not atomic with
// respect to each other, meaning if you write A then B and crash, you
// may observe B but not A. Within a single session, read-your-writes
// consistency is provided.
type BlockDB interface {

	// WriteBlock writes a block to the database. Idempotent on duplicate
	// block hash: a second WriteBlock for the same blockHash is a no-op,
	// not an error.
	//
	// This method may return immediately and does not necessarily wait for
	// the block to be written to disk. Call Flush() if you need to wait.
	WriteBlock(ctx context.Context, block Block) error

	// Flush blocks until all pending writes are durable. WriteBlocks issued
	// before calling Flush() will be crash-durable after Flush() returns.
	// Concurrent WriteBlocks may or may not be durable after Flush()
	// returns (but are otherwise eventually durable).
	Flush(ctx context.Context) error

	// GetBlockByHash retrieves a block by its hash.
	GetBlockByHash(ctx context.Context, hash []byte) (block Block, ok bool, err error)

	// GetBlockByHeight retrieves a block by its height.
	GetBlockByHeight(ctx context.Context, height uint64) (block Block, ok bool, err error)

	// Prune schedules pruning of all blocks with height < lowestHeightToKeep.
	// Pruning is asynchronous; this method does not guarantee when it will
	// complete. Some data may not be pruned if the database is closed before
	// pruning is scheduled.
	Prune(ctx context.Context, lowestHeightToKeep uint64) error

	// GetLowestBlockHeight returns the lowest block height in the database.
	GetLowestBlockHeight(ctx context.Context) (uint64, error)

	// GetHighestBlockHeight returns the highest block height in the database.
	GetHighestBlockHeight(ctx context.Context) (uint64, error)

	// Close shuts the database down and releases any resources. Any in-flight
	// writes are fully flushed to disk before this method returns.
	Close(ctx context.Context) error
}
