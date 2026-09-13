package evm

import (
	"fmt"
	"math"

	"github.com/sei-protocol/sei-chain/sei-db/controller"
)

// EVMStateStore participates in the shared prune cycle as a snapshot store: it restores only at a
// snapshot boundary, replaying the state WAL forward from there to reach any higher height.
//
// It has both dimensions the cycle prunes. Snapshots are the checkpoint directories its snapshot
// manager publishes; history is the per-version MVCC rows each managed database holds, which is what
// lets it answer a query about a past block.
//
// The state WAL must be managed by the same collector. This store reports a floor that keeps the WAL
// it replays from, but never prunes that WAL itself.
var _ controller.PrunableStore = (*EVMStateStore)(nil)

// prunableStoreName identifies this store to the collector it is pruned by and to the schedule it
// checkpoints on, which name it independently of each other.
const prunableStoreName = "EVM SS"

func (s *EVMStateStore) Name() string {
	return prunableStoreName
}

// ExternalPruning reports StateStoreConfig.ExternalPruning, the same value the snapshot manager
// consults before running its own count-based retention. It is fixed at construction.
func (s *EVMStateStore) ExternalPruning() bool {
	return s.externalPruning
}

// PruneHistory drops the MVCC history below blockNumber from every managed database, keeping
// blockNumber itself readable.
//
// A cut line at or above this store's own head is clamped to the head. Pruning to a height the store
// has not reached would drop every version it holds, leaving a store that answers no historical query
// at all, which is the failure this clamp exists to prevent.
func (s *EVMStateStore) PruneHistory(blockNumber uint64) error {
	if blockNumber == 0 {
		return nil
	}
	head, err := s.GetLatestBlock()
	if err != nil {
		return fmt.Errorf("read the EVM state store head to bound the history cut line: %w", err)
	}
	if head == 0 {
		return nil
	}
	// Prune is inclusive of the version it is given, where the cut line names the lowest version to
	// keep, so the last version dropped is the one below it.
	cutLine := min(blockNumber, head) - 1
	if cutLine == 0 {
		return nil
	}
	if cutLine > math.MaxInt64 {
		return fmt.Errorf("cannot prune EVM state store history below %d: above the highest version a store can hold", cutLine)
	}
	return s.Prune(int64(cutLine))
}

// PruneSnapshots hands blockNumber to the snapshot manager, which deletes every snapshot below it
// except the one a restore currently resolves through. A store with no snapshot manager keeps no
// snapshots and has nothing to do.
func (s *EVMStateStore) PruneSnapshots(blockNumber uint64) error {
	if blockNumber == 0 || s.snapshotMgr == nil {
		return nil
	}
	if blockNumber > math.MaxInt64 {
		return fmt.Errorf("cannot prune EVM state store snapshots below %d: above the highest version a store can hold", blockNumber)
	}
	if err := s.snapshotMgr.PruneSnapshots(int64(blockNumber)); err != nil {
		return fmt.Errorf("prune EVM state store snapshots below %d: %w", blockNumber, err)
	}
	return nil
}

// GetRollbackFloor returns the oldest snapshot this store must keep to serve a rollback of
// rollbackWindow blocks behind its own head. See snapshot.Manager.RollbackFloor for the outcomes.
//
// It returns 0 — nothing here is eligible for pruning — when this store takes no snapshots, since a
// store that cannot restore to any height must not let the shared cut line move above the history it
// still holds.
func (s *EVMStateStore) GetRollbackFloor(rollbackWindow uint64) uint64 {
	if s.snapshotMgr == nil {
		return 0
	}
	head, err := s.GetLatestBlock()
	if err != nil {
		logger.Error("failed to read the EVM state store head for the rollback floor; holding it at 0",
			"rollbackWindow", rollbackWindow, "err", err)
		return 0
	}
	return s.snapshotMgr.RollbackFloor(head, rollbackWindow)
}

// GetLatestBlock returns the highest version applied to this store, or 0 when nothing has been
// applied. This is the store's ingest position, not its newest snapshot.
func (s *EVMStateStore) GetLatestBlock() (uint64, error) {
	latest := s.GetLatestVersion()
	if latest <= 0 {
		return 0, nil
	}
	return uint64(latest), nil //nolint:gosec // guarded positive above
}
