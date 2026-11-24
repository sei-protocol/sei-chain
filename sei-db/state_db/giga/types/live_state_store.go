package types

import (
	"io"

	controllertypes "github.com/sei-protocol/sei-chain/sei-db/controller/types"
	"github.com/sei-protocol/sei-chain/sei-db/state_db/giga/types/mutation"
	"github.com/sei-protocol/sei-chain/sei-db/state_db/sc/flatkv/lthash"
)

// LiveStateStore holds the state after the latest committed block and commits each new block on top of it.
// Reading the version of a closed store and rolling it back to a recovery target are not part of it: the
// implementing package provides them alongside its opener.
//
// There are no recoverable errors: on the first error the caller must stop rather than proceed on state the
// store cannot vouch for. Byte slices passed to or received from any method must not be modified.
type LiveStateStore interface {
	controllertypes.PrunableStore
	io.Closer

	// Version returns the latest committed block, 0 when none is.
	Version() int64

	// SetInitialVersion seeds a store that has never committed so that its first commit is block
	// initialVersion. It errors on a store that has committed, or for initialVersion <= 0.
	SetInitialVersion(initialVersion int64) error

	// Get returns the value stored under key in module as of the latest committed block, and whether it was
	// found.
	Get(module string, key []byte) (value []byte, found bool)

	// OpenView returns a view of the key-values as of the latest committed block. The caller must Close it.
	OpenView() RawStateView

	// MutationsFromStateChangeSet converts block blockNum's changes into one mutation per physical key the
	// block writes, read against the latest committed block, which must be blockNum-1. The returned mutations
	// must not be modified.
	MutationsFromStateChangeSet(blockNum int64, changes *StateChangeSet) ([]mutation.Mutation, error)

	// CommitStateChanges commits block blockNum, which must be the committed version plus one. mutations holds
	// one entry per physical key the block writes, each PreviousValue being that key's value as of blockNum-1, as
	// MutationsFromStateChangeSet produces them. mutations must not be modified after the call.
	CommitStateChanges(blockNum int64, mutations []mutation.Mutation) error

	// RegisterHashListener registers listener for the hash of each block the store commits: exactly one per
	// block, in block order, with no gaps or duplicates. An error from the listener bricks the store. It returns
	// the hash of the block before the first one listener observes; a nil listener only reports that hash.
	RegisterHashListener(listener HashListener) (mostRecentHash lthash.BlockHash, err error)

	// FlushHashes blocks until every committed block's hash has been handed to every registered listener.
	FlushHashes() error
}
