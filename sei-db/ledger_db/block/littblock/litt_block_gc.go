package littblock

import (
	"github.com/sei-protocol/sei-chain/sei-db/management/gc"
	"github.com/sei-protocol/sei-chain/sei-tendermint/autobahn/types"
)

// In terms of the collector's RollbackWindow and LookbackWindow, garbage collection guarantees:
//
//  1. Garbage collection will not delete any data that is necessary to roll back to any block
//     between LatestBlock and (LatestBlock - RollbackWindow), inclusive.
//  2. Garbage collection will not delete block DB data that is at or after
//     (LatestBlock - RollbackWindow - LookbackWindow). This ensures that however far the system
//     rolls back inside the rollback window, it is still possible to read at least LookbackWindow
//     blocks of history below wherever it landed.
//  3. Garbage collection will eventually delete block data older than
//     (LatestBlock - RollbackWindow - LookbackWindow).
//
// Eventually, in guarantee 3, because PruneHistory only records a watermark: reclamation also
// waits for BlockDBConfig.RetentionTime and for LittDB's own GC to come round.
var _ gc.PrunableStore = (*blockDB)(nil)

func (s *blockDB) Name() string {
	return "BlockDB"
}

// ExternalPruning is unconditionally true: this store has no pruner of its own for the collector to
// collide with. LittDB's GC reclaims what PruneBefore has already released, on a
// config.RetentionTime timer, so it enforces no retention policy — it only carries out one this
// store has recorded.
func (s *blockDB) ExternalPruning() bool {
	return true
}

// PruneHistory advances the retention watermark to blockNumber. It only records the watermark;
// reclamation happens on LittDB's own GC schedule and no earlier than config.RetentionTime (see
// PruneBefore).
//
// blockNumber is a minimum shared across every managed store, so it may sit above this store's
// own head — a store that ingests ahead of blockDB pulls the head up, and a QC boundary can
// leave the newest retained cohort below it. PruneBefore caps the request at the newest
// retained (block, QC) pair, which is what keeps the store from emptying itself here.
func (s *blockDB) PruneHistory(blockNumber uint64) error {
	return s.PruneBefore(types.GlobalBlockNumber(blockNumber))
}

// PruneSnapshots does nothing: blockDB keeps no snapshots. It restores by reading the blocks it
// holds, so its whole retention story is the watermark PruneHistory moves.
func (s *blockDB) PruneSnapshots(uint64) error {
	return nil
}

// GetRollbackFloor returns head - rollbackWindow, the contract's answer for a contiguous store:
// every block from its floor to its head is retained, so that height is restorable directly and
// nothing below it has to be held back. It keeps no snapshots, so there is nothing to resolve the
// window against beyond its own head.
//
// It reports against its own head even when that runs ahead of the fleet's, because the collector
// takes a minimum across stores. A lagging store therefore sets the depth, and answering high here
// cannot prune anything out from under it.
//
// 0 when the window is deeper than the whole history — including the empty store, whose head is 0.
// Nothing here is eligible for pruning yet: the rollback owed reaches past genesis, so no part of the
// history can be given up until the head clears the window. Nothing is logged from here; the
// collector logs every store's answer each cycle.
func (s *blockDB) GetRollbackFloor(rollbackWindow uint64) uint64 {
	head, err := s.GetLatestBlock()
	if err != nil || head <= rollbackWindow {
		return 0 // cannot say what it holds, so nothing may be dropped anywhere
	}
	return head - rollbackWindow
}

// GetLatestBlock returns the newest block number written, or 0 when none has been.
//
// Global block numbers start at genesis block 0, so a store holding only that block is
// indistinguishable from an empty one. That is the safe direction: GetRollbackFloor then answers 0,
// which holds the fleet's history where it is, and the prune it receives is capped by PruneBefore
// to a no-op.
//
// Reports the written cursor, not the flushed one. A block that a crash would lose still counts
// as ingested — recovery re-derives this cursor from what survived, so the head can only move
// back, never past a prune that was already issued.
func (s *blockDB) GetLatestBlock() (uint64, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if !s.hasBlocks {
		return 0, nil
	}
	return uint64(s.lastBlockNumber), nil
}
