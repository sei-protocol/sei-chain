package giga

import (
	"context"

	"github.com/sei-protocol/sei-chain/sei-db/proto"
	"github.com/sei-protocol/sei-chain/sei-db/state_db/sc/flatkv/lthash"
)

// A callback function for getting the hash of each block.
type HashListener func(ctx context.Context, blockNum int64, hash *lthash.BlockHash) error

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

	// Register a callback function that that gets called for each hash produced by the database. Will be called for
	// each block in order with no gaps. Returning an error from the listener bricks the DB and will eventually
	// crash the node.
	//
	// This method returns the most recent hash observed at the moment the listener is registered. If the
	// first hash the listener observes is for block N, the mostRecentHash returned will have been block N-1.
	// This may be useful at startup time to determine the initial hash of the database.
	RegisterHashListener(listener HashListener) (mostRecentHash lthash.BlockHash, err error)
}
