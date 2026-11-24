package types

import (
	"io"

	controllertypes "github.com/sei-protocol/sei-chain/sei-db/controller/types"
)

// HistoricalStateStore records the blocks the live state store commits and serves the state after
// past ones.
type HistoricalStateStore interface {
	controllertypes.PrunableStore
	io.Closer

	// Resume binds current, a view of the live state store at version, and enables ApplyBlock. It must
	// succeed once before ApplyBlock is called. version must be at or above GetLatestVersion. It takes
	// ownership of current whether or not it succeeds.
	Resume(version int64, current StateView) error

	// Resumed reports whether a Resume has succeeded and the store is not closed.
	Resumed() bool

	// ApplyBlock records block version's changes and takes ownership of current, the live state store's
	// view after the block. It may write in the background, so changes must not be modified afterwards;
	// nil means the block changed nothing. It panics before a successful Resume or after Close.
	ApplyBlock(version int64, changes *StateChangeSet, current StateView)

	// WaitForPendingWrites blocks until every block passed to ApplyBlock so far is written.
	WaitForPendingWrites()

	// OpenView returns a view of the state after block version, and false when the store is closed or
	// does not serve version. The view holds back pruning until it is closed.
	OpenView(version int64) (StateView, bool)

	// GetLatestVersion returns the latest block whose records are written and readable.
	GetLatestVersion() int64

	// GetEarliestVersion returns the lowest height a view can open at.
	GetEarliestVersion() int64
}
