package flatkv

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"

	"github.com/sei-protocol/sei-chain/sei-db/management/gc"
)

// CommitStore participates in the shared prune cycle as a snapshot store: it restores only at a
// snapshot boundary, replaying the state WAL forward from there to reach any higher height.
//
// The state WAL must be managed by the same collector. This store reports a floor that keeps the WAL
// it replays from, but never prunes that WAL itself.
var _ gc.PrunableStore = (*CommitStore)(nil)

func (s *CommitStore) Name() string {
	return "FlatKV"
}

// ExternalPruning reports config.ExternalPruning, the same field pruneSnapshotsByCount and
// tryTruncateWAL consult to stand down. It is fixed at construction.
func (s *CommitStore) ExternalPruning() bool {
	return s.config.ExternalPruning
}

// PruneHistory does nothing: the history this store replays over is the state WAL, which the
// collector manages as a store in its own right.
func (s *CommitStore) PruneHistory(uint64) error {
	return nil
}

// PruneSnapshots deletes every snapshot strictly below blockNumber, never the active snapshot. A
// snapshot that fails to delete is reported while the rest are still attempted; one that is already
// gone is not an error.
func (s *CommitStore) PruneSnapshots(blockNumber uint64) error {
	if blockNumber == 0 {
		return nil
	}
	heights, err := s.snapshotHeights()
	if err != nil {
		return fmt.Errorf("scan snapshots: %w", err)
	}
	if len(heights) == 0 {
		return nil
	}
	active, err := s.activeSnapshotHeight()
	if err != nil {
		return fmt.Errorf("prune snapshots below %d: %w", blockNumber, err)
	}
	cutLine := min(blockNumber, active)

	var errs error
	pruned := 0
	for _, height := range heights {
		if height >= cutLine {
			break // ascending, so nothing further is a candidate
		}
		removed, err := s.deleteSnapshot(height)
		if err != nil {
			errs = errors.Join(errs, err)
			continue
		}
		if removed {
			pruned++
		}
	}

	if pruned > 0 {
		logger.Info("pruned snapshots below the rollback cut line", "count", pruned, "cutLine", cutLine)
	}
	return errs
}

// activeSnapshotHeight returns the height of the snapshot "current" points at — the one the next open
// clones and replays the state WAL forward from. It is usually the newest snapshot on disk, but a
// crash during WriteSnapshot or a partial Rollback can leave it lower.
func (s *CommitStore) activeSnapshotHeight() (uint64, error) {
	_, version, err := currentSnapshotDir(s.flatkvDir())
	if err != nil {
		return 0, fmt.Errorf("resolve active snapshot: %w", err)
	}
	return uint64(max(version, 0)), nil //nolint:gosec // clamped non-negative above
}

// snapshotFloor returns the oldest snapshot that must survive a rollback of rollbackWindow blocks
// behind head:
//
//	a snapshot at or below head - rollbackWindow → the newest such snapshot
//	every snapshot above that height             → the oldest snapshot, the deepest this store can
//	                                               restore to
//	no snapshot, or a window deeper than head    → 0, nothing here is eligible for pruning
//
// Version 0 is never the answer, as it restores to no committed height. blocks may be in any order.
func snapshotFloor(blocks []uint64, head uint64, rollbackWindow uint64) uint64 {
	if head <= rollbackWindow {
		return 0
	}
	target := head - rollbackWindow

	var oldest, newestPastWindow uint64
	foundReal := false
	for _, block := range blocks {
		if block == 0 {
			continue
		}
		if !foundReal || block < oldest {
			oldest = block
		}
		foundReal = true
		if block <= target && block > newestPastWindow {
			newestPastWindow = block
		}
	}

	if !foundReal {
		return 0
	}
	if newestPastWindow > 0 {
		return newestPastWindow
	}
	return oldest
}

// snapshotHeights returns the block number of every snapshot on disk, ascending. A missing snapshot
// directory yields no blocks rather than an error.
func (s *CommitStore) snapshotHeights() ([]uint64, error) {
	var blocks []uint64
	err := traverseSnapshots(s.flatkvDir(), true, func(version int64) (bool, error) {
		if version >= 0 {
			blocks = append(blocks, uint64(version))
		}
		return false, nil
	})
	if err != nil {
		return nil, err
	}
	return blocks, nil
}

// deleteSnapshot removes the snapshot directory for block, reporting whether this call is the one that
// removed it. An already-gone snapshot is not an error and reports false.
func (s *CommitStore) deleteSnapshot(block uint64) (bool, error) {
	path := filepath.Join(s.flatkvDir(), snapshotName(int64(block))) //nolint:gosec // block numbers are bounded well below 2^63
	if err := atomicRemoveDir(path); err != nil {
		if os.IsNotExist(err) {
			return false, nil
		}
		return false, fmt.Errorf("remove snapshot %d: %w", block, err)
	}
	return true, nil
}

// GetRollbackFloor returns the oldest snapshot this store must keep to serve a rollback of
// rollbackWindow blocks behind its own committed head, bounded by the active snapshot. See
// snapshotFloor for the outcomes.
//
// It returns 0 — nothing here is eligible for pruning — when there is no snapshot to name, when the
// window is deeper than the head, or when the snapshot layout cannot be read.
func (s *CommitStore) GetRollbackFloor(rollbackWindow uint64) uint64 {
	heights, err := s.snapshotHeights()
	if err != nil {
		logger.Error("failed to scan snapshots for the rollback floor; holding it at 0",
			"rollbackWindow", rollbackWindow, "err", err)
		return 0
	}
	head, err := s.GetLatestBlock()
	if err != nil {
		logger.Error("failed to read the committed version for the rollback floor; holding it at 0",
			"rollbackWindow", rollbackWindow, "err", err)
		return 0
	}
	floor := snapshotFloor(heights, head, rollbackWindow)
	if floor == 0 {
		return 0
	}
	active, err := s.activeSnapshotHeight()
	if err != nil {
		logger.Error("failed to resolve the active snapshot for the rollback floor; holding it at 0",
			"rollbackWindow", rollbackWindow, "err", err)
		return 0
	}
	return min(floor, active)
}

// GetLatestBlock returns the highest committed version, or 0 when nothing has been committed. This is
// the store's ingest position, not its newest snapshot.
func (s *CommitStore) GetLatestBlock() (uint64, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	if s.committedVersion <= 0 {
		return 0, nil
	}
	return uint64(s.committedVersion), nil //nolint:gosec // guarded positive above
}
