package statewal

import (
	"fmt"

	"github.com/sei-protocol/sei-chain/sei-db/management/gc"
)

// stateWALImpl joins the shared prune cycle as a contiguous store: it holds every block from its
// retention floor up to its head, so any height in between can serve a rollback.
//
// This is the only surface of stateWALImpl usable from another goroutine (see the type doc). Every
// method here reads a constant, a single atomic, or the WAL underneath.
//
// SC and SS must be managed by the same collector. They replay from this WAL, and their floors are
// what hold it back, since the collector prunes every store to the shared minimum.
var _ gc.PrunableStore = (*stateWALImpl)(nil)

func (w *stateWALImpl) Name() string {
	return "StateWAL"
}

// ExternalPruning is unconditionally true: the WAL prunes only when told to, so it has no pruner of
// its own for the collector to collide with.
func (w *stateWALImpl) ExternalPruning() bool {
	return true
}

// PruneHistory schedules removal of the blocks below blockNumber, going straight to the underlying
// WAL. Prune is the equivalent for the WAL's own owner; this one is safe to call from the collector's
// goroutine, and leaves the WAL usable on failure rather than bricking it.
func (w *stateWALImpl) PruneHistory(blockNumber uint64) error {
	if err := w.wal.PruneBefore(blockNumber); err != nil {
		return fmt.Errorf("failed to prune state WAL below block %d: %w", blockNumber, err)
	}
	return nil
}

// PruneSnapshots does nothing: the WAL keeps no snapshots.
func (w *stateWALImpl) PruneSnapshots(uint64) error {
	return nil
}

// GetRollbackFloor returns head - rollbackWindow, measured against this WAL's own head. It returns
// 0 — nothing here is eligible for pruning — when the window is deeper than the whole WAL, which
// includes a freshly created one, or when the head cannot be read.
func (w *stateWALImpl) GetRollbackFloor(rollbackWindow uint64) uint64 {
	head, err := w.GetLatestBlock()
	if err != nil {
		return 0
	}
	if head <= rollbackWindow {
		return 0
	}
	return head - rollbackWindow
}

// GetLatestBlock returns the highest block ended by SignalEndOfBlock, or 0 when none has been. A
// block written but not yet ended is excluded: it is still buffered rather than a record.
func (w *stateWALImpl) GetLatestBlock() (uint64, error) {
	return w.lastCompletedBlock.Load(), nil
}
