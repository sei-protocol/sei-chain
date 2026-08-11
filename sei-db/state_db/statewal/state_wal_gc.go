package statewal

import (
	"fmt"

	"github.com/sei-protocol/sei-chain/sei-db/management/gc"
)

// stateWALImpl joins the shared prune cycle as a contiguous store: it holds every block from its
// retention floor up to its head, so any height in between can serve a rollback.
//
// This is the only surface of stateWALImpl that may be used from another goroutine (see the type
// doc). Every method here reads a constant, a single atomic, or the WAL underneath — which permits a
// concurrent PruneBefore by contract. None of them touch the plain fields the writer owns.
//
// Precondition beyond the collector's own: SC and SS must be managed alongside this WAL. The WAL is
// what they replay from, and it is their floors — the oldest snapshot each still needs — that hold it
// back, since the collector prunes every store to the shared minimum. Managed without them, the WAL
// is pruned to its own floor and the replay range they depend on goes with it.
var _ gc.PrunableStore = (*stateWALImpl)(nil)

func (w *stateWALImpl) Name() string {
	return "StateWAL"
}

// ExternalPruning is unconditionally true: the WAL prunes only when told to, by Prune or PruneBelow,
// so there is never a pruner inside it for the collector to collide with.
//
// FlatKV does prune this WAL directly today, in tryTruncateWAL, which can look like the store
// enforcing its own retention and so like a reason to answer false. It is not. This method is read
// only from a prune cycle, so it has an effect only where a collector exists — and FlatKV stands
// tryTruncateWAL down under its own config.ExternalPruning precisely so the collector can take the
// WAL over. False would leave that handover with nothing on either side of it: tryTruncateWAL
// stopped, the collector declining, and the WAL growing without bound.
//
// Where FlatKV has not handed over, both prune, and the shared minimum makes that safe rather than
// merely redundant: a store is asked for its floor whatever it answers here, so FlatKV holds the
// collector back to the oldest snapshot it still replays from. That relies on FlatKV being
// registered at all, which is the precondition on the type above.
func (w *stateWALImpl) ExternalPruning() bool {
	return true
}

// PruneHistory schedules removal of the blocks below blockNumber, going straight to the underlying
// WAL from the collector's goroutine. Prune is the equivalent for the WAL's own owner; this exists
// separately only because it must not read the closed/fatalErr bookkeeping that Prune does.
//
// blockNumber is the shared minimum rather than this WAL's own boundary, which for this store is the
// whole point: SC and SS answer their oldest live snapshot, and that minimum is what holds the WAL
// back far enough for them to replay forward from it.
//
// Deliberately does not brick the WAL on failure, unlike every writer-goroutine path here. Bricking
// means writing fatalErr, which the writer reads unsynchronized, so doing it from this goroutine would
// be a data race. Nothing is lost by declining: a prune fails only when the WAL is already closed or
// dead underneath, and the writer's next operation discovers that on its own.
func (w *stateWALImpl) PruneHistory(blockNumber uint64) error {
	if err := w.wal.PruneBefore(blockNumber); err != nil {
		return fmt.Errorf("failed to prune state WAL below block %d: %w", blockNumber, err)
	}
	return nil
}

// PruneSnapshots does nothing: the WAL keeps no snapshots. It is what snapshots are replayed
// forward from, which reaches it as the shared minimum PruneHistory receives.
func (w *stateWALImpl) PruneSnapshots(uint64) error {
	return nil
}

// GetRollbackFloor returns head - rollbackWindow, the contract's answer for a contiguous store:
// the WAL holds every block from its floor to its head, so that height is replayable directly and
// nothing below it has to be held back. It keeps no snapshots — it is what snapshots replay from — so
// there is nothing to resolve the window against beyond its own head.
//
// It reports against its own head even when that runs ahead of the fleet's, because the collector
// takes a minimum across stores. This WAL's own answer is rarely the binding one: SC and SS report
// their oldest live snapshot, which sits below this and is what actually holds the WAL back far
// enough for them to replay forward.
//
// 0 when the window is deeper than the whole WAL — including a freshly created one, whose head is 0.
// Nothing here is eligible for pruning until the head clears the window.
func (w *stateWALImpl) GetRollbackFloor(rollbackWindow uint64) uint64 {
	head, err := w.GetLatestBlock()
	if err != nil {
		return 0 // cannot say what it holds, so nothing may be dropped anywhere
	}
	if head <= rollbackWindow {
		return 0
	}
	return head - rollbackWindow
}

// GetLatestBlock returns the highest block ended by SignalEndOfBlock, or 0 when none has been.
//
// A block that has been written but not yet ended is deliberately excluded: it is still buffered,
// not a record, and reporting it would put the floor this store reports one block above what the WAL
// can actually replay. See stateWALImpl.lastCompletedBlock for the 0 case.
func (w *stateWALImpl) GetLatestBlock() (uint64, error) {
	return w.lastCompletedBlock.Load(), nil
}
