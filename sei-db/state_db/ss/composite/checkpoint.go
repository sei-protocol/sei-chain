package composite

import (
	"github.com/sei-protocol/sei-chain/sei-db/controller"
)

var _ controller.CheckpointableStore = (*CompositeStateStore)(nil)

// ScheduleCheckpoint records targetVersion as the height the next SS snapshot is taken at. The
// snapshot itself is the one the write path already stages and publishes across the members; this
// only fixes which height that happens on.
//
// A store with snapshots disabled, stopped, or already holding a target ignores the request, as does
// a height at or below the last snapshot this coordinator requested.
func (s *CompositeStateStore) ScheduleCheckpoint(targetVersion int64) {
	if s.snapshotMgr == nil {
		return
	}
	s.snapshotMgr.acceptTarget(targetVersion)
}

// LatestVersion returns the newest version this store has committed. It reports the same height as
// GetLatestVersion, which is the state store's own interface; this is the name the checkpoint
// scheduler reads it under.
func (s *CompositeStateStore) LatestVersion() int64 {
	return s.GetLatestVersion()
}

// CheckpointInProgress reports whether a snapshot is currently being written.
func (s *CompositeStateStore) CheckpointInProgress() bool {
	return s.snapshotMgr.checkpointInProgress()
}
