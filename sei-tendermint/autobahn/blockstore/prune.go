package blockstore

import (
	"fmt"

	"github.com/sei-protocol/sei-chain/sei-db/controller"
	blocktypes "github.com/sei-protocol/sei-chain/sei-db/ledger_db/block/types"
	"github.com/sei-protocol/sei-chain/sei-tendermint/autobahn/types"
)

// The prune policy lives here rather than in the block database because both
// facts it turns on are encoded in the values, which the database holds as
// opaque bytes: where the QC covering a block begins, and how far the
// application has committed. A database that had to answer either would have to
// index the whole store and rebuild that index on every open.

// A block store is what the storage garbage collector prunes.
var _ controller.PrunableStore = (*Store)(nil)

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
	return s.PruneBefore(types.GlobalBlockNumber(blockNumber))
}

// RetainingStore is a block store whose history the collector never prunes closer than a fixed
// number of blocks behind its head.
type RetainingStore struct {
	*Store
	retention uint64
}

var _ controller.PrunableStore = RetainingStore{}

// Retaining returns s as the collector prunes it when at least retention blocks behind the head
// must stay readable. A retention of 0 prunes wherever the collector asks.
func (s *Store) Retaining(retention uint64) RetainingStore {
	return RetainingStore{Store: s, retention: retention}
}

// PruneHistory prunes below blockNumber, lowered so that at least retention blocks behind the head
// are kept. It prunes nothing while the whole store is within the retention.
func (s RetainingStore) PruneHistory(blockNumber uint64) error {
	head, err := s.GetLatestBlock()
	if err != nil {
		return err
	}
	if head <= s.retention {
		return nil
	}
	return s.Store.PruneHistory(min(blockNumber, head-s.retention))
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
// written cursor rather than the flushed one, so a block a crash would lose still counts as ingested.
//
// Global block numbers start at genesis block 0, so a store holding only that block is
// indistinguishable from an empty one.
func (s *Store) GetLatestBlock() (uint64, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if status, ok := s.status.Get(); ok && types.GlobalBlockNumber(s.db.GetPruneWatermark()) < status.NextBlock {
		return uint64(status.NextBlock - 1), nil
	}
	return 0, nil
}

// clampPruneBoundary returns the start of the QC that covers n, or n if there is no QC covering N
// (which can happen if you prune the same n twice).
func (s *Store) clampPruneBoundary(blockHeight types.GlobalBlockNumber) (types.GlobalBlockNumber, error) {
	value, exists, err := s.db.GetRecord(blocktypes.KindQC, uint64(blockHeight))
	if err != nil {
		return 0, fmt.Errorf("failed to read covering QC for %d: %w", blockHeight, err)
	}
	if !exists {
		return blockHeight, nil
	}
	qc, err := decodeQC(value)
	if err != nil {
		return 0, fmt.Errorf("failed to decode covering QC for %d: %w", blockHeight, err)
	}
	return qc.QC().GlobalRange().First, nil
}
