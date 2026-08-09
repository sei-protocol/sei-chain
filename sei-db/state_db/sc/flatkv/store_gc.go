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
// must hold to serve a rollback is not a block range but the newest snapshot at or below the target,
// which is what GetPruningBoundary reports and what holds the WAL back for it.
//
// Participation is conditional on config.ExternalPruning, which is what stands the store's own two
// pruners down — see ExternalPruning below. With it unset the store is still asked for its boundary,
// and still protects the WAL it replays from, but enforces its own retention by snapshot count.
//
// Precondition beyond the collector's own: the state WAL must be managed alongside this store. The
// snapshot alone only restores the exact height it was taken at; every height above it needs the WAL
// blocks that follow. Managed without the WAL, this store answers a boundary nothing acts on while the
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

// ExternalPruning reports config.ExternalPruning, the same field pruneSnapshots and tryTruncateWAL
// consult to stand down. Reading one field from all three is what makes "the collector prunes this
// store" and "this store does not prune itself" a single fact: there is no combination of settings
// that turns both on, so the count-based pruner can never delete a snapshot the collector is holding.
//
// It is a construction-time value, so the answer is stable for the life of the store and safe to read
// from the collector's goroutine.
func (s *CommitStore) ExternalPruning() bool {
	return s.config.ExternalPruning
}

// PruneBelow deletes every snapshot below blockNumber, leaving the WAL alone — the collector prunes
// that directly as its own store.
//
// The boundary snapshot this store reported survives, because blockNumber is a minimum across stores
// and so never exceeds that boundary (see GetPruningBoundary). It is therefore always left with at
// least one snapshot to restore from.
//
// Deletion is not all-or-nothing: a snapshot that fails to delete is reported and the rest are still
// attempted, since each snapshot directory is independent and a single undeletable one must not strand
// the disk space of the others. Already-gone snapshots are not an error — the SnapshotKeepRecent
// pruner in WriteSnapshot deletes from the same set, and losing that race means the work is done.
func (s *CommitStore) PruneBelow(blockNumber uint64) error {
	if blockNumber == 0 {
		return nil
	}

	dir := s.flatkvDir()

	// The active snapshot is what the next open resolves to, so it is never a candidate however deep the
	// request. Nothing in the contract should produce such a request — a boundary is never above the
	// snapshot it names — but the cost of the check is one readlink against making a wrong answer
	// elsewhere unbootable here.
	_, activeVersion, err := currentSnapshotDir(dir)
	if err != nil {
		return fmt.Errorf("resolve active snapshot before pruning: %w", err)
	}

	var errs error
	pruned := 0
	scanErr := traverseSnapshots(dir, true, func(version int64) (bool, error) {
		if uint64(version) >= blockNumber { //nolint:gosec // snapshot versions are non-negative
			return true, nil // ascending, so nothing further is a candidate
		}
		if version == activeVersion {
			return false, nil
		}
		if err := atomicRemoveDir(filepath.Join(dir, snapshotName(version))); err != nil {
			if !os.IsNotExist(err) {
				errs = errors.Join(errs, fmt.Errorf("remove snapshot %d: %w", version, err))
			}
			return false, nil
		}
		pruned++
		return false, nil
	})
	if scanErr != nil {
		errs = errors.Join(errs, fmt.Errorf("scan snapshots: %w", scanErr))
	}
	// One line per cycle rather than one per snapshot: the count is the whole story here, since the
	// set pruned is always "everything below the floor". A cycle that prunes nothing is the common
	// case and says nothing, so it stays silent.
	if pruned > 0 {
		logger.Info("pruned snapshots below retention floor", "count", pruned, "floor", blockNumber)
	}
	return errs
}

// GetRetentionWindow reports 0: this store asks for no history of its own beyond the collector's shared
// rollback window, which is the contract for a snapshot store (see gc.PrunableStore).
//
// 0 is what keeps it in the shared minimum, and being in that minimum is how its snapshots are
// protected — it answers its oldest needed snapshot as a boundary and the WAL is held there with it.
// InfiniteRetentionWindow would do the opposite of what it reads like: a store with no cut line is
// never asked for a boundary, so the WAL would be pruned to its own cut line and the snapshots left
// with nothing to replay from.
//
// How deep this store can actually restore is a function of SnapshotInterval and SnapshotKeepRecent,
// not of anything declared here. Those two decide which snapshots exist; if they retain less than
// RollbackWindow of history, the window is not servable no matter what this returns.
func (s *CommitStore) GetRetentionWindow() int64 {
	return 0
}

// GetPruningBoundary returns the newest snapshot at or below cutLine — the oldest point this store must
// keep to restore to cutLine, since restoring to anything above that snapshot replays the WAL forward
// from it.
//
// When every snapshot sits above cutLine it returns cutLine, per the contract: none of them can be
// dropped, so holding the other stores back to the oldest of them would buy nothing. Note what this
// means operationally — the store cannot in fact restore to cutLine in that case, only to its oldest
// snapshot. That is a snapshot-retention shortfall (SnapshotInterval × SnapshotKeepRecent shallower
// than RollbackWindow), and reporting it here as a boundary would not fix it, it would only stall
// every other store's pruning while the gap persisted.
//
// The initial empty snapshot at version 0 is not a boundary. It restores to no committed height, and
// reaching any height from it requires replaying the WAL from the very first block — which is exactly
// CannotServeRollback, both in meaning and in the value 0 would collide with.
//
// A failed directory scan is also reported as CannotServeRollback, abandoning the cycle. Not knowing
// which snapshots exist means not knowing which blocks are needed to replay from them, and the WAL is
// what would be pruned on the strength of a guess.
func (s *CommitStore) GetPruningBoundary(cutLine uint64) uint64 {
	var newestAtOrBelow, newest int64
	err := traverseSnapshots(s.flatkvDir(), false, func(version int64) (bool, error) {
		if version < 1 {
			return false, nil
		}
		if newest == 0 {
			newest = version // descending, so the first is the newest
		}
		if uint64(version) <= cutLine { //nolint:gosec // guarded >= 1 above
			newestAtOrBelow = version
			return true, nil
		}
		return false, nil
	})
	if err != nil {
		logger.Error("failed to scan snapshots for pruning boundary; blocking the prune cycle",
			"cutLine", cutLine, "err", err)
		return gc.CannotServeRollback
	}

	switch {
	case newestAtOrBelow > 0:
		return uint64(newestAtOrBelow) //nolint:gosec // guarded > 0
	case newest > 0:
		return cutLine
	default:
		return gc.CannotServeRollback
	}
}

// GetLatestBlock returns the highest committed version, or 0 when nothing has been committed.
//
// This is the committed version rather than the newest snapshot: it is the store's ingest position,
// which is what the collector takes a minimum over to find the fleet's head. The snapshot layout only
// enters through GetPruningBoundary.
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
