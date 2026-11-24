// Package types declares the contracts the Giga EVM executor reads and writes state through —
// StateDB, LiveStateStore, HistoricalStateStore, RawStateView and StateView — together with the EVM
// value types in their signatures.
// It holds no implementation, so a store and its callers can both depend on it.
package types

import (
	"context"

	"github.com/sei-protocol/sei-chain/sei-db/state_db/sc/flatkv/lthash"
)

// A callback function for getting the hash of each block.
type HashListener func(ctx context.Context, blockNum int64, hash *lthash.BlockHash) error

// StateDB is the top-level API the Giga EVM executor reads and writes state through. A commit goes to the
// live state store and, when it is enabled, the historical state store; reads are served for the latest
// block, or for a past block the historical state store holds.
type StateDB interface {

	// Ingest a block's state changes.
	CommitStateChanges(blockNum int64, changes *StateChangeSet) error

	// OpenView returns a read-only view of the latest committed block. The caller must Close it.
	OpenView() StateView

	// OpenViewAt returns a read-only StateView for the given
	// committed block height. The bool is false when no
	// view exists at that height. When true, the caller must Close the
	// returned view when done.
	OpenViewAt(blockNum int64) (StateView, bool)

	// Register a callback function that gets called for each hash produced by the database. Will be called for
	// each block in order with no gaps. Returning an error from the listener bricks the DB and will eventually
	// crash the node.
	//
	// This method returns the most recent hash observed at the moment the listener is registered. If the
	// first hash the listener observes is for block N, the mostRecentHash returned will have been block N-1.
	// This may be useful at startup time to determine the initial hash of the database.
	RegisterHashListener(listener HashListener) (mostRecentHash lthash.BlockHash, err error)

	// Close releases everything this StateDB was built over, reporting every failure rather than
	// stopping at the first.
	Close() error
}
