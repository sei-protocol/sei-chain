package receipt

import (
	"github.com/sei-protocol/sei-chain/sei-db/controller"
)

// littReceiptStore joins the shared prune cycle as a contiguous store: it holds every block from its
// retention floor up to its head, so any height in between can serve a rollback.
//
// Only the littidx backend participates. The pebble backend delegates to the state store, which
// prunes on its own KeepRecent schedule.
var _ controller.PrunableStore = (*littReceiptStore)(nil)

func (s *littReceiptStore) Name() string {
	return "ReceiptDB"
}

// ExternalPruning reports config.ExternalPruning, the same value runsLocalPruner consults before
// starting the KeepRecent pruner. It is fixed at construction, and false for a store running without
// a collector.
func (s *littReceiptStore) ExternalPruning() bool {
	return s.externalPruning
}

// PruneHistory advances the retention floor to blockNumber and drops the tag-index entries below it.
// Receipt bodies are released to litt's GC rather than deleted here, and are reclaimed once they are
// also past the TTL (see gcFilter). Reads below the floor return not-found in the meantime.
func (s *littReceiptStore) PruneHistory(blockNumber uint64) error {
	return s.pruneBlocksBelow(blockNumber)
}

// PruneSnapshots does nothing: this store keeps no snapshots.
func (s *littReceiptStore) PruneSnapshots(uint64) error {
	return nil
}

// GetRollbackFloor returns head - rollbackWindow, measured against this store's own head. It returns
// 0 — nothing here is eligible for pruning — when the window is deeper than the whole history, which
// includes a store that has written nothing, or when the head cannot be read.
func (s *littReceiptStore) GetRollbackFloor(rollbackWindow uint64) uint64 {
	head, err := s.GetLatestBlock()
	if err != nil || head <= rollbackWindow {
		return 0
	}
	return head - rollbackWindow
}

// GetLatestBlock returns the newest block whose receipts have been written, or 0 when none have.
func (s *littReceiptStore) GetLatestBlock() (uint64, error) {
	latest := s.latestVersion.Load()
	if latest <= 0 {
		return 0, nil
	}
	return uint64(latest), nil //nolint:gosec // guarded non-negative above
}
