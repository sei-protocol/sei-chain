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

// ExternalPruning reports config.ExternalPruning, and runsLocalPruner asks this same method before
// starting the KeepRecent pruner. One read behind both is what makes "the collector prunes this
// store" and "this store does not prune itself" a single fact, so the local pruner can never race
// the collector to a shallower floor and delete the rollback headroom it is holding.
//
// It is not unconditionally true because this store still runs without a collector — a node with
// rs-backend = "littidx" and KeepRecent from min-retain-blocks depends on the local pruner, and
// standing that down with nothing to replace it grows the tag index without bound.
//
// It is a construction-time value, so the answer is stable for the life of the store and safe to
// read from the collector's goroutine.
func (s *littReceiptStore) ExternalPruning() bool {
	return s.externalPruning
}

// PruneHistory advances the retention floor to blockNumber and drops the tag-index entries below
// it. Receipt bodies are not deleted here: advancing the floor is what releases them to litt's GC,
// which reclaims them once they are also past the TTL (see gcFilter). Reads below the floor return
// not-found in the meantime (see belowRetentionFloor), so visible retention follows this call even
// when reclamation lags it — and because the floor gates reclamation, it can never lead it.
//
// KeepRecent does not enter into this. It is the window the store's own pruner uses when it runs,
// and the collector calling here means that pruner stood down (see ExternalPruning); how deep to
// prune is then the collector's fleet-wide RollbackWindow and LookbackWindow, not a per-store
// setting.
func (s *littReceiptStore) PruneHistory(blockNumber uint64) error {
	return s.pruneBlocksBelow(blockNumber)
}

// PruneSnapshots does nothing: this store keeps no snapshots. Receipts are read from the blocks
// themselves, so the retention floor PruneHistory moves is the whole story.
func (s *littReceiptStore) PruneSnapshots(uint64) error {
	return nil
}

// GetRollbackFloor returns head - rollbackWindow, the contract's answer for a contiguous store:
// every block from the floor to the head is retained, so that height is restorable directly and
// nothing below it has to be held back. This store keeps no snapshots, so there is nothing to resolve
// the window against beyond its own head.
//
// It reports against its own head even when that runs ahead of the fleet's, because the collector
// takes a minimum across stores.
//
// 0 when the window is deeper than the whole history — including a store still backfilling, whose
// head is 0. Nothing here is eligible for pruning until the head clears the window.
func (s *littReceiptStore) GetRollbackFloor(rollbackWindow uint64) uint64 {
	head, err := s.GetLatestBlock()
	if err != nil || head <= rollbackWindow {
		return 0 // cannot say what it holds, so nothing may be dropped anywhere
	}
	return head - rollbackWindow
}

// GetLatestBlock returns the newest block whose receipts have been written, or 0 when none have.
// A store still filling then answers a rollback floor of 0, which holds the fleet's history where it
// is — the right trade, since the prune it would otherwise receive only moves a floor that has no
// data under it.
func (s *littReceiptStore) GetLatestBlock() (uint64, error) {
	latest := s.latestVersion.Load()
	if latest <= 0 {
		return 0, nil
	}
	return uint64(latest), nil //nolint:gosec // guarded non-negative above
}
