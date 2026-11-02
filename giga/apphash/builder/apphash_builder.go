package builder

import (
	"context"

	"github.com/sei-protocol/sei-chain/giga/apphash"
)

// AppHashBuilder computes the app hash of each block from inputs reported independently, and serves the app hashes
// by block height. In order to provide protection against equivocation, it never reveals an app hash before it is
// durable (for a configurable definition of durable).
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
	ReportReceiptHash(ctx context.Context, blockHeight uint64, receiptHash apphash.ReceiptHash) error

	// Returns the app hash of the block, or nil if it is not available yet. An available app hash never changes.
	// Reading blocks in ascending order is cheap; reading them in random order is expensive.
	//
	// Errors if called before SetupComplete(), if blockHeight is below the giga activation height or has been
	// pruned, or once the builder stops.
	TryGetAppHash(blockHeight uint64) (*apphash.AppHashData, error)

	// Returns the app hash of the block, blocking until it is available. May be called before SetupComplete(). An
	// available app hash never changes. Reading blocks in ascending order is cheap; reading them in random order is
	// expensive.
	//
	// Errors if ctx is cancelled, if blockHeight is below the giga activation height or has been pruned, or once
	// the builder stops.
	GetAppHash(ctx context.Context, blockHeight uint64) (*apphash.AppHashData, error)

	// Returns lowest and highest blocks whose app hashes are currently available.
	Bounds() (
		// The lowest block whose app hash is available. Valid only if ok.
		lowest uint64,
		// The highest block whose app hash is available. Valid only if ok.
		highest uint64,
		// False if no app hash is available yet.
		ok bool,
		err error,
	)

	// Marks the end of startup. Afterwards, the app hash of every block through blockHeight is available.
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

	// Stores the app hash of every block whose inputs were all reported before the call, then stops the builder and
	// returns once its resources are released. A report made concurrently with the call may or may not be stored. A
	// builder stopped by an error stores nothing more.
	Close() error
}
