package flatkv

import (
	"github.com/sei-protocol/sei-chain/sei-db/controller"
)

var _ controller.CheckpointableStore = (*CommitStore)(nil)

// ScheduleCheckpoint records targetVersion as the version this store's next snapshot is written at.
// The snapshot itself is the one Commit already writes; this only fixes which block it happens on.
//
// A height at or below the committed version, a request on a read-only store, or a second request
// while one is already pending is ignored.
func (s *CommitStore) ScheduleCheckpoint(targetVersion int64) {
	s.mu.RLock()
	readOnly := s.readOnly
	committed := s.committedVersion
	s.mu.RUnlock()

	if readOnly || targetVersion <= committed {
		return
	}
	s.pendingCheckpoint.CompareAndSwap(0, targetVersion)
}

// LatestVersion returns the version this store has committed.
func (s *CommitStore) LatestVersion() int64 {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.committedVersion
}

// CheckpointInProgress reports whether this store is currently writing a snapshot.
func (s *CommitStore) CheckpointInProgress() bool {
	return s.snapshotInProgress.Load()
}

// snapshotIfDue writes a snapshot when version is one this store owes a snapshot at. Two cadences can
// ask for one — the store-local SnapshotInterval, and a target a controller.CheckpointScheduler
// dispatched — and a version both name is one snapshot, not two.
//
// Commit calls this while holding the write lock, so the snapshot it writes is of exactly the block
// that just committed.
func (s *CommitStore) snapshotIfDue(version int64) {
	scheduled := s.consumeCheckpointTarget(version)
	periodic := s.config.SnapshotInterval > 0 && version%int64(s.config.SnapshotInterval) == 0
	if !scheduled && !periodic {
		return
	}
	s.phaseTimer.SetPhase("commit_write_snapshot")
	s.snapshotInProgress.Store(true)
	defer s.snapshotInProgress.Store(false)
	if err := s.WriteSnapshot(""); err != nil {
		logger.Error("auto snapshot failed", "version", version, "err", err)
	}
}

// consumeCheckpointTarget reports whether version is the accepted checkpoint target, clearing the
// target once version has reached it.
//
// It clears on an overshoot as well as on a match: a target left set at a version this store has
// already passed is one no later commit can match. Overshooting is a bug rather than a race — Commit
// takes contiguous versions and a target is only accepted above the committed one — so it is
// reported rather than absorbed.
func (s *CommitStore) consumeCheckpointTarget(version int64) bool {
	target := s.pendingCheckpoint.Load()
	if target == 0 || version < target {
		return false
	}
	if !s.pendingCheckpoint.CompareAndSwap(target, 0) {
		return false
	}
	if version > target {
		logger.Error("FlatKV passed a scheduled checkpoint version without snapshotting it",
			"targetVersion", target, "committedVersion", version)
		return false
	}
	return true
}
