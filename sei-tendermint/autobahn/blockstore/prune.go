package blockstore

import (
	controllertypes "github.com/sei-protocol/sei-chain/sei-db/controller/types"

	"github.com/sei-protocol/sei-chain/sei-tendermint/autobahn/types"
)

// The prune policy lives here because eligibility depends on how far the
// application has committed; the block database stores opaque records.

// A block store is what the storage garbage collector prunes.
var _ controllertypes.PrunableStore = (*Store)(nil)

// Name identifies the store in the garbage collector's logs and errors.
func (s *Store) Name() string {
	return "BlockDB"
}

// ExternalPruning is unconditionally true: this store has no pruner of its own.
func (s *Store) ExternalPruning() bool {
	return true
}

// PruneHistory advances the retention watermark to blockNumber, capping it at the newest retained
// (block, QC) pair. Reclaiming the data happens later, on the block database's own schedule.
func (s *Store) PruneHistory(blockNumber uint64) error {
	return s.PruneBefore(types.RoadIndex(blockNumber))
}

// PruneSnapshots does nothing: the block store keeps no snapshots.
func (s *Store) PruneSnapshots(uint64) error {
	return nil
}

// GetRollbackFloor returns head - rollbackWindow, measured against this store's own head. It returns
// 0 — nothing here is eligible for pruning — when the window is deeper than the whole history, which
// includes the empty store, or when the head cannot be read.
func (s *Store) GetRollbackFloor(rollbackWindow uint64) uint64 {
	head, err := s.GetLatestBlock()
	if err != nil || head <= rollbackWindow {
		return 0
	}
	return head - rollbackWindow
}

// GetLatestBlock returns the newest block number written, or 0 when none has been. It reports the
// written status rather than the flushed one, so a block a crash would lose still counts as ingested.
//
// Road indices can start at 0, so a store holding only that block is
// indistinguishable from an empty one.
func (s *Store) GetLatestBlock() (uint64, error) {
	for status := range s.status.Lock() {
		if current, ok := status.Get(); ok && types.RoadIndex(s.db.GetPruneWatermark()) < current.NextQC {
			return uint64(current.NextQC - 1), nil
		}
		return 0, nil
	}
	panic("unreachable")
}
