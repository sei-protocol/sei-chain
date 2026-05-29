package hashvault

import (
	"context"
	"errors"
)

// HashVault is a safety mechanism to prevent a validator from "changing its mind" about the hash of a block
// without human intervention.
type HashVault interface {

	// CommitToHash takes a provided hash for a block and writes it to disk. This method blocks until the hash is
	// crash durable.
	//
	// This utility may be passed the hash for a block multiple times, but it will refuse to allow the hash to change
	// for a particular block height. If this method returns nil, then it means that the hash is either the first
	// one observed by the HashVault, or that the hash is the same as one for this block that was previously reported.
	//
	// If this method returns an error, DO NOT ATTEMPT TO RECOVER WITHOUT HUMAN INTERVENTION!
	CommitToHash(ctx context.Context, blockHeight uint64, hash []byte) error

	// Prune deletes all data for blocks below the specified height. Keeps data for the specified block height.
	// Note that reporting the hash for a block below the pruning boundary will result in an error
	// (as it is impossible to validate the correctness of the hash for a block below the pruning boundary).
	Prune(ctx context.Context, blockHeight uint64) error

	// Close shuts the HashVault down and frees all resources (but does not delete the data from disk).
	Close(ctx context.Context) error
}

// BlockHashSize is the required byte length for hashes passed to CommitToHash (CometBFT block ID / header hash).
const BlockHashSize = 32

// ErrInvalidHashLength is returned when CommitToHash is called with a hash whose length is not BlockHashSize.
var ErrInvalidHashLength = errors.New("block hash must be 32 bytes")

// ErrHashMismatch is returned by CommitToHash when the caller provides a hash that differs from the
// hash previously committed for the same block height. This is the primary "node changed its mind"
// signal and MUST cause the calling node to halt.
var ErrHashMismatch = errors.New("block hash mismatch")

// ErrBelowPruneBoundary is returned when an operation targets a block height that has already been
// pruned. Reporting or rolling back through pruned heights is impossible to validate and so is rejected.
var ErrBelowPruneBoundary = errors.New("block height below prune boundary")

// ErrClosed is returned when a method is called after Close.
var ErrClosed = errors.New("hashvault is closed")

// ErrCorruption is returned when the on-disk integrity check (SHA-256 trailer bound to (height, hash))
// fails. This indicates either disk corruption or a bug in the encoding layer. Callers MUST treat
// this as fatal and require human intervention.
var ErrCorruption = errors.New("hashvault on-disk integrity check failed")

// ErrRollbackHeightOverflow is returned by HardRollbackPebbleHashVault when blockHeight is
// math.MaxUint64. The partial-rollback path deletes hashes strictly above blockHeight via
// DeleteRange(hashKey(blockHeight+1), ...); at MaxUint64 that addition wraps to zero and would
// delete the entire vault instead of none.
var ErrRollbackHeightOverflow = errors.New("rollback block height overflows uint64")
