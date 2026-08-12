package littblock

import (
	"github.com/sei-protocol/sei-chain/sei-db/management/gc"
	"github.com/sei-protocol/sei-chain/sei-tendermint/autobahn/types"
)

// blockDB participates in the shared prune cycle as a contiguous store: it holds every block from its
// retention watermark to its head, so any height in that range is restorable directly.
var _ gc.PrunableStore = (*blockDB)(nil)

func (s *blockDB) Name() string {
	return "BlockDB"
}

// ExternalPruning is unconditionally true: this store has no pruner of its own.
func (s *blockDB) ExternalPruning() bool {
	return true
}

// PruneHistory advances the retention watermark to blockNumber, capping it at the newest retained
// (block, QC) pair. Reclaiming the data happens later, on LittDB's GC schedule and no earlier than
// BlockDBConfig.RetentionTime.
func (s *blockDB) PruneHistory(blockNumber uint64) error {
	return s.PruneBefore(types.GlobalBlockNumber(blockNumber))
}

// PruneSnapshots does nothing: blockDB keeps no snapshots.
func (s *blockDB) PruneSnapshots(uint64) error {
	return nil
}

// GetRollbackFloor returns head - rollbackWindow, measured against this store's own head. It returns
// 0 — nothing here is eligible for pruning — when the window is deeper than the whole history, which
// includes the empty store, or when the head cannot be read.
func (s *blockDB) GetRollbackFloor(rollbackWindow uint64) uint64 {
	head, err := s.GetLatestBlock()
	if err != nil || head <= rollbackWindow {
		return 0
	}
	return head - rollbackWindow
}

// GetLatestBlock returns the newest block number written, or 0 when none has been. It reports the
// written cursor rather than the flushed one, so a block a crash would lose still counts as ingested.
//
// Global block numbers start at genesis block 0, so a store holding only that block is
// indistinguishable from an empty one.
func (s *blockDB) GetLatestBlock() (uint64, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if status, ok := s.status.Get(); ok && types.GlobalBlockNumber(s.watermark.Load()) < status.NextBlock {
		return uint64(status.NextBlock - 1), nil
	}
	return 0, nil
}
