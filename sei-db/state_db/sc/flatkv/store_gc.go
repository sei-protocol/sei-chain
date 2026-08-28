package flatkv

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"go.opentelemetry.io/otel/metric"

	"github.com/sei-protocol/sei-chain/sei-db/controller"
)

// CommitStore participates in the shared prune cycle as a snapshot store: it restores only at a
// snapshot boundary, replaying the state WAL forward from there to reach any higher height.
//
// The state WAL must be managed by the same collector. This store reports a floor that keeps the WAL
// it replays from, but never prunes that WAL itself.
var _ controller.PrunableStore = (*CommitStore)(nil)

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

// PruneSnapshots hands blockNumber to the snapshot writer as a retention cut line and returns
// without waiting for the deletion. It reports only that the cut line was refused, which happens
// when the writer has already failed.
//
// Deletion runs on the writer's goroutine because that goroutine is the only one allowed to mutate
// the snapshot tree on a live store: it is also what publishes snapshots into that tree. Doing it
// here instead would race a publication that is renaming a directory in and moving "current".
func (s *CommitStore) PruneSnapshots(blockNumber uint64) error {
	if blockNumber == 0 {
		return nil
	}
	writer := s.currentSnapshotWriter()
	if writer == nil {
		return nil
	}
	if err := writer.PruneBelow(blockNumber); err != nil {
		return fmt.Errorf("hand cut line %d to the snapshot writer: %w", blockNumber, err)
	}
	return nil
}

// currentSnapshotWriter returns the writer this store currently holds, or nil when it has none: a
// read-only store, a closed one, or one between closeStores and openStores. A cycle that finds nil
// is skipped, the next one carrying a cut line at or above the one it dropped.
func (s *CommitStore) currentSnapshotWriter() *SnapshotWriter {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.snapshotWriter
}

// pruneSnapshotsBelow deletes every snapshot under dir strictly below cutLine, never the active
// snapshot. A snapshot that fails to delete is reported while the rest are still attempted; one
// that is already gone is not an error.
//
// ctx carries only the metric recording; the deletion itself is not cancellable.
func pruneSnapshotsBelow(ctx context.Context, dir string, cutLine uint64) error {
	start := time.Now()
	defer func() {
		otelMetrics.SnapshotPruneLatency.Record(ctx, secondsSince(start))
	}()

	blocks, err := snapshotBlocks(dir)
	if err != nil {
		return fmt.Errorf("scan snapshots: %w", err)
	}
	if len(blocks) == 0 {
		return nil
	}
	active, err := activeSnapshotHeight(dir)
	if err != nil {
		return fmt.Errorf("bound the cut line by the active snapshot: %w", err)
	}
	// The active snapshot is what the next open resolves through, so the cut line stops there
	// however deep it was asked to go.
	bounded := min(cutLine, active)

	var errs error
	pruned := 0
	for _, block := range blocks {
		if block >= bounded {
			break // ascending, so nothing further is a candidate
		}
		removed, err := deleteSnapshot(dir, block)
		otelMetrics.SnapshotPruneAttempts.Add(ctx, 1, metric.WithAttributes(successAttr(err)))
		if err != nil {
			errs = errors.Join(errs, err)
			continue
		}
		if removed {
			pruned++
		}
	}

	if pruned > 0 {
		logger.Info("pruned snapshots below the rollback cut line", "count", pruned, "cutLine", bounded)
	}
	return errs
}

// activeSnapshotHeight returns the height of the snapshot "current" points at under dir — the one
// the next open clones and replays the state WAL forward from. It is usually the newest snapshot on
// disk, but a crash while a snapshot was being published, or a partial Rollback, can leave it lower.
func activeSnapshotHeight(dir string) (uint64, error) {
	_, version, err := currentSnapshotDir(dir)
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

// snapshotBlocks returns the block number of every snapshot under dir, ascending. A missing snapshot
// directory yields no blocks rather than an error.
func snapshotBlocks(dir string) ([]uint64, error) {
	var blocks []uint64
	err := traverseSnapshots(dir, true, func(version int64) (bool, error) {
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

// deleteSnapshot removes the snapshot directory for block under dir, reporting whether this call is
// the one that removed it. An already-gone snapshot is not an error and reports false.
func deleteSnapshot(dir string, block uint64) (bool, error) {
	path := filepath.Join(dir, snapshotName(int64(block))) //nolint:gosec // block numbers are bounded well below 2^63
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
	blocks, err := snapshotBlocks(s.flatkvDir())
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
	floor := snapshotFloor(blocks, head, rollbackWindow)
	if floor == 0 {
		return 0
	}
	active, err := activeSnapshotHeight(s.flatkvDir())
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
