package builder

import (
	"context"

	"github.com/sei-protocol/sei-chain/giga/apphash"
)

// AppHashBuilder computes the app hash of each block from inputs reported independently, and publishes the app
// hashes in block order. App hashes start at the giga activation height, which must be a global chain configuration
// value. No app hash is published below the giga activation height.
type AppHashBuilder interface {

	// Reports the hash of the block.
	//
	// blockHeight must be one above the previous call's, except on the first call. Illegal input to this method
	// may not cause it to return an error immediately, since hash data may be ingested asynchronously. Illegal
	// input will, however, eventually cause the app hash builder to shut down and start returning errors from all
	// methods.
	ReportBlockHash(ctx context.Context, blockHeight uint64, blockHash [32]byte) error

	// Reports the state hash after executing the block, along with debugging hashes of the state's parts.
	//
	// blockHeight must be one above the previous call's, except on the first call. Illegal input to this method
	// may not cause it to return an error immediately, since hash data may be ingested asynchronously. Illegal
	// input will, however, eventually cause the app hash builder to shut down and start returning errors from all
	// methods.
	ReportStateHash(
		ctx context.Context,
		blockHeight uint64,
		stateHash [32]byte,
		// Named hashes of the parts of the state, such as each flatKV database and module (see
		// hashlog.FlatKVComponentHashes()). They exist for debugging only: they are written to the hash
		// log beside the block's app hash, are never part of the app hash, and have no effect on it. May
		// be nil. Must not be mutated after the call.
		componentHashes map[string][]byte,
	) error

	// Reports the BUD hash (Block Update Digest hash) of the block.
	//
	// blockHeight must be one above the previous call's, except on the first call. Illegal input to this method
	// may not cause it to return an error immediately, since hash data may be ingested asynchronously. Illegal
	// input will, however, eventually cause the app hash builder to shut down and start returning errors from all
	// methods.
	ReportBUDHash(ctx context.Context, blockHeight uint64, budHash [32]byte) error

	// Reports the hash of the transaction receipts produced by executing the block.
	//
	// blockHeight must be one above the previous call's, except on the first call. Illegal input to this method
	// may not cause it to return an error immediately, since hash data may be ingested asynchronously. Illegal
	// input will, however, eventually cause the app hash builder to shut down and start returning errors from all
	// methods.
	ReportReceiptHash(ctx context.Context, blockHeight uint64, receiptHash [32]byte) error

	// Marks the end of startup, which publishes the app hashes through blockHeight.
	//
	// Must be called once, after every call made during startup to the methods that report hashes (i.e. the
	// Report*() methods), though some of them may never have been called. Afterwards, each Report*() method's next
	// call must be for blockHeight+1. Errors if called again, or if an app hash at or below blockHeight cannot be
	// computed; either error shuts the builder down.
	SetupComplete(
		ctx context.Context,
		// The height of the storage layer: the last block every store has applied.
		blockHeight uint64,
	) error

	// Registers listener to receive, in block order, every app hash published after the one returned.
	//
	// Returns the newest published app hash, or nil if none has been published (this is possible if the builder
	// is started at a block height below the giga activation height). A published app hash never changes.
	// Errors if listener is nil, or if called before SetupComplete().
	RegisterListener(
		ctx context.Context,
		// Called once the app hash is stored; the store is durable only if the builder's configuration requests
		// it. It may delay later app hashes while it runs, and it must not call the builder.
		listener func(appHash *apphash.AppHashData),
	) (*apphash.AppHashData, error)

	// Returns an iterator over the published app hashes from startingBlockHeight through the newest one. No app
	// hash exists below the giga activation height, so iteration from a lower height starts there.
	//
	// A published app hash never changes. Errors if called before SetupComplete(), or if startingBlockHeight has
	// been pruned. The iterator errors once the builder stops.
	Iterator(ctx context.Context, startingBlockHeight uint64) (apphash.AppHashIterator, error)

	// Stores and publishes the app hash of every block whose inputs were all reported before the call, then stops
	// the builder and returns once its resources are released. A report made concurrently with the call may or may
	// not be stored. A builder stopped by an error stores nothing more. Every iterator must be closed first.
	Close() error
}
