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
// what they replay from, and it is their boundaries — the oldest snapshot each still needs — that
// hold it back, since the collector prunes every store to the shared minimum. Managed without them,
// the WAL is pruned to its own cut line and the replay range they depend on goes with it.
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
// merely redundant: a store is asked for its boundary whatever it answers here, so FlatKV holds the
// collector back to the oldest snapshot it still replays from. That relies on FlatKV being
// registered at all, which is the precondition on the type above.
func (w *stateWALImpl) ExternalPruning() bool {
	return true
}

// PruneBelow schedules removal of the blocks below blockNumber, going straight to the underlying WAL
// from the collector's goroutine. Prune is the equivalent for the WAL's own owner; this exists
// separately only because it must not read the closed/fatalErr bookkeeping that Prune does.
//
// Deliberately does not brick the WAL on failure, unlike every writer-goroutine path here. Bricking
// means writing fatalErr, which the writer reads unsynchronized, so doing it from this goroutine would
// be a data race. Nothing is lost by declining: a prune fails only when the WAL is already closed or
// dead underneath, and the writer's next operation discovers that on its own.
func (w *stateWALImpl) PruneBelow(blockNumber uint64) error {
	if err := w.wal.PruneBefore(blockNumber); err != nil {
		return fmt.Errorf("failed to prune state WAL below block %d: %w", blockNumber, err)
	}
	return nil
}

// GetRetentionWindow reports 0, and this is a property of what the WAL is rather than a default
// something might want to raise. How deep this WAL must go is not its own to declare: it is a replay
// source, and the depth it needs is whatever its consumers need it to be. SC and SS express that by
// answering their oldest live snapshot as a pruning boundary, and the collector prunes every store
// to the shared minimum, so the WAL is already held exactly as far back as they can replay from.
//
// A window here would be additive on top of that, and — because the minimum is shared — it would
// retain every managed store that much further back rather than this WAL alone. That is a fleet-wide
// retention decision wearing a per-store name, which is what RollbackWindow already is.
func (w *stateWALImpl) GetRetentionWindow() int64 {
	return 0
}

// GetPruningBoundary returns cutLine, the contract's answer for a contiguous store: the WAL holds
// every block from its floor to its head, so cutLine itself is replayable and nothing below it has
// to be held back.
//
// Unconditional on purpose. A WAL whose floor already sits above cutLine — pruned there by an
// earlier cycle, or freshly created above it — also answers cutLine, because the PruneBelow that
// follows is a no-op on it while a lower answer would hold every other store back to this WAL's
// floor. CannotServeRollback is never right here: it is the replay source, not a replay consumer.
func (w *stateWALImpl) GetPruningBoundary(cutLine uint64) uint64 {
	return cutLine
}

// GetLatestBlock returns the highest block ended by SignalEndOfBlock, or 0 when none has been.
//
// A block that has been written but not yet ended is deliberately excluded: it is still buffered,
// not a record, and reporting it would put the collector's head one block above what the WAL can
// actually replay. See stateWALImpl.lastCompletedBlock for the 0 case.
func (w *stateWALImpl) GetLatestBlock() (uint64, error) {
	return w.lastCompletedBlock.Load(), nil
}
