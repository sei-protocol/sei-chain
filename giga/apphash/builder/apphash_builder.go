package builder

import (
	"context"

	"github.com/sei-protocol/sei-chain/giga/apphash"
)

// AppHashBuilder computes the app hash of each block from inputs reported independently, and publishes the app
// hashes in block order.
type AppHashBuilder interface {

	// Reports the hash of the block.
	//
	// blockHeight must be one above the previous call's, except on the first call. Illegal input to this method
	// may not cause it to return an error immediately, since hash data may be ingested asynchronously. Illegal
	// input will, however, eventually cause the app hash builder to shut down and start returning errors from all
	// methods.
	ReportBlockHash(ctx context.Context, blockHeight uint64, blockHash [32]byte) error

	// Reports the state hash after executing the block.
	//
	// blockHeight must be one above the previous call's, except on the first call. Illegal input to this method
	// may not cause it to return an error immediately, since hash data may be ingested asynchronously. Illegal
	// input will, however, eventually cause the app hash builder to shut down and start returning errors from all
	// methods.
	ReportStateHash(ctx context.Context, blockHeight uint64, stateHash [32]byte) error

	// Reports the Block Update Digest of the block.
	//
	// blockHeight must be one above the previous call's, except on the first call. Illegal input to this method
	// may not cause it to return an error immediately, since hash data may be ingested asynchronously. Illegal
	// input will, however, eventually cause the app hash builder to shut down and start returning errors from all
	// methods.
	ReportBUD(ctx context.Context, blockHeight uint64, bud [32]byte) error

	// Reports the hash of the transaction receipts produced by executing the block.
	//
	// blockHeight must be one above the previous call's, except on the first call. Illegal input to this method
	// may not cause it to return an error immediately, since hash data may be ingested asynchronously. Illegal
	// input will, however, eventually cause the app hash builder to shut down and start returning errors from all
	// methods.
	ReportReceiptHash(ctx context.Context, blockHeight uint64, receiptHash [32]byte) error

	// Marks the end of startup, which publishes the app hashes through blockHeight.
	//
	// Must be called after every call made during startup to the methods that report hashes (i.e. the Report*()
	// methods), though some of them may never have been called. Afterwards, each Report*() method's next call must
	// be for blockHeight+1. Errors if an app hash at or below blockHeight cannot be computed.
	SetupComplete(
		ctx context.Context,
		// The height of the storage layer: the last block every store has applied.
		blockHeight uint64,
	) error

	// Registers listener to receive, in block order, every app hash published after the one returned.
	//
	// Returns the newest published app hash, or nil if none has been published. A published app hash never
	// changes. Errors if listener is nil, or if called before SetupComplete().
	RegisterListener(
		ctx context.Context,
		// Called once the app hash is durable. It may delay later app hashes while it runs, and it must not
		// call the builder.
		listener func(appHash *apphash.AppHashData),
	) (*apphash.AppHashData, error)

	// Returns an iterator over the published app hashes from startingBlockHeight through the newest one.
	//
	// A published app hash never changes. Errors if called before SetupComplete(), or if startingBlockHeight has
	// been pruned.
	Iterator(ctx context.Context, startingBlockHeight uint64) (apphash.AppHashIterator, error)

	// Permits deleting the app hashes below blockHeight.
	//
	// They may be deleted at any later time, and the newest published app hash is always kept.
	Prune(ctx context.Context, blockHeight uint64) error

	// Stops the builder and releases its resources.
	//
	// This method does not flush data, and may cause in-flight work to be dropped. Every iterator must be closed
	// first.
	Close() error
}
