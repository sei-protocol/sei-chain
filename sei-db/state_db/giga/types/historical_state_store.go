package types

import (
	"io"

	controllertypes "github.com/sei-protocol/sei-chain/sei-db/controller/types"
	"github.com/sei-protocol/sei-chain/sei-db/state_db/giga/types/mutation"
)

// HistoricalStateStore records the blocks the live state store commits and serves the state after past ones.
type HistoricalStateStore interface {
	controllertypes.PrunableStore
	io.Closer

	// Resume binds current, the live state store's view at version, and must succeed once before ApplyBlock.
	// version must be at or above GetLatestVersion. It takes ownership of current whether or not it succeeds.
	Resume(version int64, current RawStateView) error

	// Resumed reports whether a Resume has succeeded and the store is not closed.
	Resumed() bool

	// ApplyBlock records block version's mutations and takes ownership of current, the live state store's view
	// after the block. mutations must not be modified afterwards. It panics before a successful Resume, after
	// Close, or on a mutation the live state store could not have committed.
	ApplyBlock(version int64, mutations []mutation.Mutation, current RawStateView)

	// ReplayBlock records block version's mutations, read back from the state WAL, before Resume. version must be
	// the block after GetLatestVersion. It errors after Resume or Close, or on a mutation the live state store
	// could not have committed.
	ReplayBlock(version int64, mutations []mutation.Mutation) error

	// WaitForPendingWrites blocks until every block passed to ApplyBlock so far is written.
	WaitForPendingWrites()

	// OpenView returns a view of the state after block version, and false when the store is closed or
	// does not serve version. The view holds back pruning until it is closed.
	OpenView(version int64) (RawStateView, bool)

	// GetLatestVersion returns the latest block whose records are written and readable.
	GetLatestVersion() int64

	// GetEarliestVersion returns the lowest height a view can open at.
	GetEarliestVersion() int64
}
