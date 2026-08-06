package littblock

import (
	"github.com/sei-protocol/sei-chain/sei-db/management/gc"
	"github.com/sei-protocol/sei-chain/sei-tendermint/autobahn/types"
)

// blockDB joins the shared prune cycle as a contiguous store: everything from its retention
// floor up to its head is retained, so it can serve a rollback to any height in between. The
// collector owns the decision of how deep to prune; PruneBefore stays the direct entry point
// for callers holding a types.BlockDB.
var _ gc.PrunableStore = (*blockDB)(nil)

func (s *blockDB) Name() string {
	return "BlockDB"
}

// ExternalPruning is unconditionally true: this store has no pruner of its own for the collector to
// collide with. LittDB's GC reclaims what PruneBefore has already released, on a config.Retention
// timer, so it enforces no retention policy — it only carries out one this store has recorded.
func (s *blockDB) ExternalPruning() bool {
	return true
}

// PruneBelow advances the retention watermark to blockNumber. It only records the watermark;
// reclamation happens on LittDB's own GC schedule and no earlier than config.Retention (see
// PruneBefore).
//
// blockNumber is a minimum shared across every managed store, so it may sit above this store's
// own head — a store that ingests ahead of blockDB pulls the head up, and a QC boundary can
// leave the newest retained cohort below it. PruneBefore caps the request at the newest
// retained (block, QC) pair, which is what keeps the store from emptying itself here.
func (s *blockDB) PruneBelow(blockNumber uint64) error {
	return s.PruneBefore(types.GlobalBlockNumber(blockNumber))
}

// GetRetentionWindow reports the configured history beyond the collector's shared rollback
// window. See BlockDBConfig.RetentionWindow for the meaning of each value, and note that it is
// an input to a fleet-wide minimum rather than a policy applied to this store alone.
func (s *blockDB) GetRetentionWindow() int64 {
	if s.config.RetentionWindow < 0 {
		return gc.InfiniteRetentionWindow
	}
	return s.config.RetentionWindow
}

// GetPruningBoundary returns cutLine, the contract's answer for a contiguous store: every block
// at or above the watermark is retained, so cutLine itself is restorable and nothing below it
// has to be held back.
//
// Unconditional on purpose. A store whose floor already sits above cutLine — bootstrapped
// mid-chain, or pruned there by an earlier cycle — still answers cutLine, because the
// PruneBefore that follows is a no-op on it while a lower answer would hold back every other
// store. CannotServeRollback is never right here: this store fills from its own ingest path,
// so it has no replay range for another store's data to protect.
func (s *blockDB) GetPruningBoundary(cutLine uint64) uint64 {
	return cutLine
}

// GetLatestBlock returns the newest block number written, or 0 when none has been.
//
// Global block numbers start at genesis block 0, so a store holding only that block is
// indistinguishable from an empty one and is excluded from the collector's head. That is the
// safe direction: it drops out of the head minimum rather than dragging every store's cut line
// to 0, and the prune it then receives is capped by PruneBefore to a no-op.
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
