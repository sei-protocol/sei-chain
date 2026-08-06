package receipt

import "github.com/sei-protocol/sei-chain/sei-db/management/gc"

// littReceiptStore joins the shared prune cycle as a contiguous store: it holds every block from
// its retention floor up to its head, so any height in between can serve a rollback.
//
// Only the littidx backend participates. The pebble backend delegates to the state store, which
// prunes on its own KeepRecent schedule.
var _ gc.PrunableStore = (*littReceiptStore)(nil)

func (s *littReceiptStore) Name() string {
	return "ReceiptDB"
}

// ExternalPruning is unconditionally true: the background pruner this store used to run was removed
// when it joined the collector, precisely because it pruned to latest-KeepRecent with no knowledge of
// the shared rollback window. Nothing here prunes on its own any more.
func (s *littReceiptStore) ExternalPruning() bool {
	return true
}

// PruneBelow advances the retention floor to blockNumber and drops the tag-index entries below
// it. Receipt bodies are not deleted here: litt expires them by TTL, and reads below the floor
// return not-found in the meantime (see belowRetentionFloor), so visible retention follows this
// call even when reclamation lags it.
func (s *littReceiptStore) PruneBelow(blockNumber uint64) error {
	return s.pruneBlocksBelow(blockNumber)
}

// GetRetentionWindow translates KeepRecent into the collector's window.
//
// The two disagree on what 0 means, and the disagreement is the whole reason this is not a plain
// field read: KeepRecent 0 means "keep everything, never prune" (it is derived from
// min-retain-blocks, and 0 is the default), while a 0 here means "keep nothing beyond the shared
// RollbackWindow" — the most aggressive answer available. Returning it verbatim would prune a
// store configured to retain forever back to the rollback window. gc.InfiniteRetentionWindow is
// the sentinel that carries the intended meaning, and it is a legitimate answer for a contiguous
// store, which holds no replay range another store depends on.
//
// Negative values are folded into the same case: KeepRecent is never negative in practice, and
// this store has historically read <= 0 as "pruning off", so folding them keeps that reading
// intact rather than passing an out-of-contract value to the collector.
func (s *littReceiptStore) GetRetentionWindow() int64 {
	if s.keepRecent <= 0 {
		return gc.InfiniteRetentionWindow
	}
	return s.keepRecent
}

// GetPruningBoundary returns cutLine, the contract's answer for a contiguous store: every block
// at or above the floor is retained, so cutLine itself is restorable and nothing below it has to
// be held back.
//
// Unconditional on purpose. A store whose floor already sits above cutLine — pruned there by an
// earlier cycle, or still backfilling — also answers cutLine, because the PruneBelow that follows
// is a no-op on it while a lower answer would hold every other store back to this store's floor.
// CannotServeRollback is never right here: receipts are written from this node's own execution
// path, so no other store replays out of them.
func (s *littReceiptStore) GetPruningBoundary(cutLine uint64) uint64 {
	return cutLine
}

// GetLatestBlock returns the newest block whose receipts have been written, or 0 when none have.
// 0 keeps the store out of the collector's head minimum rather than dragging every store's cut
// line down to it — the right trade while a store is still filling, since the prune it then
// receives only moves a floor that has no data under it.
func (s *littReceiptStore) GetLatestBlock() (uint64, error) {
	latest := s.latestVersion.Load()
	if latest <= 0 {
		return 0, nil
	}
	return uint64(latest), nil //nolint:gosec // guarded non-negative above
}
