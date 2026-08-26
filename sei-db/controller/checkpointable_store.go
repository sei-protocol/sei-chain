package controller

// CheckpointableStore is a store whose checkpoints are scheduled by the CheckpointScheduler.
//
// A checkpoint is a point-in-time snapshot of the store at one version. The scheduler decides which
// version that is; a store decides how to create it.
type CheckpointableStore interface {
	// ScheduleCheckpoint records that this store should checkpoint at targetVersion. It returns once
	// the request is recorded; the store performs the checkpoint when its own write path reaches that
	// version. A height the store cannot take — already at or past it, or one already pending — is
	// ignored rather than failed: the scheduler has sent the task, and a wrong height must not
	// become a nearby height instead.
	ScheduleCheckpoint(targetVersion int64)

	// LatestVersion returns the newest version this store has committed, 0 when it has committed
	// nothing. The scheduler picks a target above every store's answer, so a store that reports a
	// version it has not durably reached is asking to be handed a target it will never see.
	LatestVersion() int64

	// CheckpointInProgress reports whether a checkpoint this store is writing has yet to finish.
	CheckpointInProgress() bool
}
