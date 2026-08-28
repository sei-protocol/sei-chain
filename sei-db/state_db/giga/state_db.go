package giga

import "github.com/sei-protocol/sei-chain/sei-db/proto"

// StateDB is the top-level API used by the Giga EVM executor for
// read and write. Writes commit into both SC and SS; reads can be served for
// the current block or for a past block (if retained by the SS).
type StateDB interface {

	// CommitStateChanges writes the given changesets into both SC and SS.
	// Call after executing each block. Hash computation and disk I/O may
	// continue asynchronously, but the state changes must be visible to
	// subsequent reads as soon as this function returns.
	CommitStateChanges(blockNum int64, changeset []*proto.NamedChangeSet) error

	// OpenView returns a read-only StateView of the current block
	// (backed by an SC ephemeral snapshot). The caller must Close it when done.
	OpenView() StateView

	// OpenViewAt returns a read-only StateView for the given
	// committed block height (backed by SS). The bool is false when no
	// view exists at that height. When true, the caller must Close the
	// returned view when done.
	OpenViewAt(blockNum int64) (StateView, bool)
}
