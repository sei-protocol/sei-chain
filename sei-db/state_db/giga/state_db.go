package giga

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
}
