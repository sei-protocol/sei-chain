package flatkv

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"

	"github.com/sei-protocol/sei-chain/sei-db/management/gc"
)

// CommitStore joins the shared prune cycle as a snapshot store: it can restore only at a snapshot
// boundary, replaying the state WAL forward from there to reach any higher height. So the history it
// must hold to serve a rollback is not a block range but a single snapshot, which is what
// GetRollbackFloor reports and what holds the WAL back for it.
//
// Participation is conditional on config.ExternalPruning, which is what stands the store's own two
// pruners down — see ExternalPruning below. With it unset the store is still asked for its floor, and
// still protects the WAL it replays from, but enforces its own retention by snapshot count.
//
// Precondition beyond the collector's own: the state WAL must be managed alongside this store. The
// snapshot alone only restores the exact height it was taken at; every height above it needs the WAL
// blocks that follow. Managed without the WAL, this store answers a floor nothing acts on while the
// WAL is pruned on its own schedule — and under ExternalPruning tryTruncateWAL has stood down too, so
// nothing bounds that WAL at all.
//
// Two of these methods run on the collector's goroutine while the store commits on another, so both
// avoid the plain fields Commit mutates: GetLatestBlock takes the read lock, and the snapshot-directory
// methods read the filesystem, which is already the synchronization boundary between snapshot writes
// and everything that reads them.
var _ gc.PrunableStore = (*CommitStore)(nil)

func (s *CommitStore) Name() string {
	return "FlatKV"
}

// ExternalPruning reports config.ExternalPruning, the same field pruneSnapshotsByCount and
// tryTruncateWAL consult to stand down. Reading one field from all three is what makes "the collector
// prunes this store" and "this store does not prune itself" a single fact: there is no combination of
// settings that turns both on, so the count-based pruner can never delete a snapshot the collector is
// holding.
//
// It is a construction-time value, so the answer is stable for the life of the store and safe to read
// from the collector's goroutine.
func (s *CommitStore) ExternalPruning() bool {
	return s.config.ExternalPruning
}

// PruneHistory does nothing: what this store replays over is the state WAL, and the collector manages
// that as a store in its own right, pruning it to the minimum across every store rather than to what
// this one alone still needs. Truncating it from here would apply a narrower view to a WAL that SS
// replays from too.
func (s *CommitStore) PruneHistory(uint64) error {
	return nil
}

// PruneSnapshots deletes every snapshot strictly below blockNumber. Restoring to any height at or
// above it starts from a snapshot at or above it and replays the WAL forward, which is what makes
// the ones underneath restore points nothing can ask for.
//
// The snapshot this store reported from GetRollbackFloor is never a candidate: blockNumber is the
// minimum across every store, so it sits at or below that answer. Keeping the reported restore point
// therefore follows from the collector's reduction and needs no guard here. The active snapshot is a
// separate question, and pruneCutLine is where that one is answered.
//
// Deletion is not all-or-nothing: a snapshot that fails to delete is reported and the rest are still
// attempted, since each snapshot directory is independent and a single undeletable one must not strand
// the disk space of the others. Already-gone snapshots are not an error — the SnapshotKeepRecent
// pruner in WriteSnapshot deletes from the same set, and losing that race means the work is done.
func (s *CommitStore) PruneSnapshots(blockNumber uint64) error {
	if blockNumber == 0 {
		return nil // nothing is eligible; the collector does not call with this
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

	// One line per cycle rather than one per snapshot: the count is the whole story here, since the
	// set pruned is always "everything below the cut line". A cycle that prunes nothing is the common
	// case and says nothing, so it stays silent.
	if pruned > 0 {
		logger.Info("pruned snapshots below the rollback cut line", "count", pruned, "cutLine", cutLine)
	}
	return errs
}

// activeSnapshotHeight returns the height of the snapshot "current" points at: the one the next open
// clones into the working directory and replays the state WAL forward from.
//
// Both halves of this store's GC surface are bounded by it, and for the same reason — this is where
// the store will actually resume from, whatever else is on disk:
//
//	the floor it reports must not sit above it, or the collector prunes the WAL past the blocks
//	    that open needs to replay
//	the cut line it prunes to must not either, or the deletion takes the very snapshot open
//	    resolves to
//
// Normally it is the newest snapshot, so both bounds are slack and neither changes an answer.
// Normally, not always — two ordinary failures leave "current" below the newest directory on disk:
//
//	a crash between WriteSnapshot's rename and its symlink update, which leaves the new snapshot
//	    on disk with "current" still on the previous one; the next open takes the symlink, so the
//	    newer directory stays an orphan rather than being adopted
//	a rollback that fails partway, which repoints "current" at the rollback base and only clears
//	    the snapshots above it at the very end
//
// Both bounds fail quietly if they are missing, which is why they are worth their cost: os.Readlink
// resolves a dangling symlink, so a store that lost its active snapshot opens against a directory
// that is not there rather than failing where the mistake was made.
func (s *CommitStore) activeSnapshotHeight() (uint64, error) {
	_, version, err := currentSnapshotDir(s.flatkvDir())
	if err != nil {
		return 0, fmt.Errorf("resolve active snapshot: %w", err)
	}
	return uint64(max(version, 0)), nil //nolint:gosec // clamped non-negative above
}

// snapshotFloor picks the oldest snapshot that has to survive a rollback of rollbackWindow blocks
// behind head. 0 means nothing here is eligible for pruning.
//
//	a snapshot at or below head - rollbackWindow → the newest such snapshot, since restoring
//	    anywhere inside the window starts from it and replays the WAL forward, and the ones below
//	    it are restore points nothing can ask for
//	every snapshot above that height → the oldest snapshot, which is then the deepest this store
//	    can restore to at all
//	no snapshot at all, or a window deeper than the head → 0
//
// The second case is a snapshot-retention shortfall: the oldest snapshot on disk is newer than
// head - rollbackWindow, so the store cannot in fact restore as deep as it is being asked to. This
// is the normal state early on, before snapshots reach that far back, and can also arise from a
// large SnapshotInterval relative to the window. Reporting its oldest snapshot is what keeps that
// shortfall from compounding — the collector holds every store's history back to it, so the one
// snapshot this store does have stays replayable. It resolves on its own as the chain advances and
// snapshots accumulate below the window.
//
// Version 0 is never the answer. It restores to no committed height, so it is not a restore point at
// all, and it is itself a candidate for deletion once a real snapshot sits above it. A store holding
// only that snapshot therefore reports 0, the same as one holding none.
//
// blocks may arrive in any order: both answers are chosen by value, not by position, so the result
// does not rest on a sort the caller happens to do. Answering too high is the one damaging direction
// here — it would let the WAL be pruned past blocks a restore replays — so the floor is not left to a
// distant invariant.
func snapshotFloor(blocks []uint64, head uint64, rollbackWindow uint64) uint64 {
	if head <= rollbackWindow {
		return 0 // the rollback owed reaches past genesis, so no snapshot can be given up
	}
	target := head - rollbackWindow

	var oldest, newestPastWindow uint64
	foundReal := false
	for _, block := range blocks {
		if block == 0 {
			continue // version 0 restores to no committed height, so it is never a floor
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
		return 0 // no real snapshot at all
	}
	if newestPastWindow > 0 {
		return newestPastWindow
	}
	return oldest
}

// snapshotHeights returns the block number of every snapshot on disk, ascending. A missing snapshot
// directory yields no blocks rather than an error, matching traverseSnapshots: a store that has not
// snapshotted yet has nothing to prune, which is not a failure.
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

// deleteSnapshot removes the snapshot directory for block, reporting whether this call is the one
// that removed it.
//
// An already-gone snapshot is success rather than an error: the SnapshotKeepRecent pruner in
// WriteSnapshot deletes from the same set, and losing that race means the work is done. It reports
// false in that case, so the cycle's count stays the number of snapshots this call reclaimed.
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
// rollbackWindow blocks behind its own head — normally the newest snapshot at or below that depth,
// since restoring anywhere above a snapshot replays the WAL forward from it. See snapshotFloor for
// every outcome.
//
// The snapshot named here survives the prune that follows, because the collector's cut line is the
// minimum across stores and so is at or below this answer.
//
// It measures against its own committed head rather than a height handed down by the collector, so a
// store running ahead of the fleet reports from where it actually is. The answer is bounded by the
// active snapshot, because a floor above where this store will resume from would let the WAL be pruned
// past the blocks that resume needs (see activeSnapshotHeight).
//
// 0 where there is no snapshot to name: nothing here is eligible for pruning, because a store that
// cannot restore anywhere cannot say which blocks the WAL may drop. Both "no snapshot on disk" and a
// window deeper than the head land there, as does a store that has committed nothing. The initial
// empty snapshot at version 0 does not count as one, because it restores to no committed height.
//
// A failed scan reports 0 for the same reason: not knowing which snapshots exist, or which one is
// active, means not knowing which blocks are needed to replay from them, and the WAL is what would be
// pruned on the strength of the guess.
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
		return 0 // nothing is eligible, so there is no bound to apply
	}
	active, err := s.activeSnapshotHeight()
	if err != nil {
		logger.Error("failed to resolve the active snapshot for the rollback floor; holding it at 0",
			"rollbackWindow", rollbackWindow, "err", err)
		return 0
	}
	return min(floor, active)
}

// GetLatestBlock returns the highest committed version, or 0 when nothing has been committed.
//
// This is the committed version rather than the newest snapshot: it is the store's ingest position,
// which is the head this store measures the rollback window against. The snapshot layout enters
// through snapshotFloor.
//
// Takes the read lock because Commit advances this field under the write lock, and the collector reads
// it from its own goroutine.
func (s *CommitStore) GetLatestBlock() (uint64, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	if s.committedVersion <= 0 {
		return 0, nil
	}
	return uint64(s.committedVersion), nil //nolint:gosec // guarded positive above
}
