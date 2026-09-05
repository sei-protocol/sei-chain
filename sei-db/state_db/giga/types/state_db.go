// Package types declares the contracts the Giga EVM executor reads and writes state through —
// StateDB, LiveStateStore and StateView — together with the EVM value types in their signatures.
// It holds no implementation, so a store and its callers can both depend on it.
package types

import "github.com/sei-protocol/sei-chain/sei-db/proto"

// StateDB is the top-level API used by the Giga EVM executor for
// read and write. Writes commit into both SC and SS; reads can be served for
// the current block or for a past block (if retained by the SS).
type StateDB interface {

	// Ingest key-value pair changes for a block.
	CommitStateChanges(blockNum int64, changeset []*proto.NamedChangeSet) error

	// OpenView returns a read-only StateView of the current block
	// (backed by an SC ephemeral snapshot). The caller must Close it when done.
	OpenView() StateView

	// OpenViewAt returns a read-only StateView for the given
	// committed block height. The bool is false when no
	// view exists at that height. When true, the caller must Close the
	// returned view when done.
	OpenViewAt(blockNum int64) (StateView, bool)

	// RollbackTo rewinds committed state to blockNum, discarding every block above it. The store must
	// be quiesced: no commit, read or open view may be in flight.
	//
	// An implementation over a WAL prunes and reopens it, so any WAL reference the caller holds is
	// closed by this call. For that reason it must not run once the prune cycle has taken the WAL.
	RollbackTo(blockNum int64) error

	// Close releases everything this StateDB was built over, reporting every failure rather than
	// stopping at the first.
	Close() error
}
