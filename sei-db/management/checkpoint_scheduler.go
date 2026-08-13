// Package management holds the coordination layer above the DB engines: work
// that decides when an engine-level operation runs, rather than how the engine
// performs it.
package management

import (
	"errors"
	"fmt"

	"github.com/sei-protocol/sei-chain/sei-db/db_engine/types"
)

// CheckpointScheduler coordinates checkpoints for stores with in-flight writes.
//
// The engine-side capabilities this builds on — types.Checkpointable,
// types.DrainBarrier and types.CheckpointVersionSetter — stay with the engines
// that implement them. What lives here is the decision of when a checkpoint runs
// and what version it is labeled with.
type CheckpointScheduler interface {
	SupportsCheckpoint() bool
	ScheduleCheckpoint(destDir string, shouldRun func() bool, done func(error))
	SetCheckpointVersion(destDir string, version int64) error
}

// ErrCheckpointCanceled reports that a queued checkpoint was canceled before
// it started.
var ErrCheckpointCanceled = errors.New("state store checkpoint canceled")

// ScheduleCheckpoint checkpoints an engine after all writes already enqueued
// on it have been applied.
func ScheduleCheckpoint(db types.StateStore, destDir string, shouldRun func() bool, done func(error)) {
	cp, ok := db.(types.Checkpointable)
	if !ok {
		done(fmt.Errorf("state store backend %T does not support checkpoints", db))
		return
	}
	barrier, ok := db.(types.DrainBarrier)
	if !ok {
		done(fmt.Errorf("state store backend %T does not support ordered checkpoint barriers", db))
		return
	}
	barrier.ScheduleAtDrain(func() {
		if shouldRun != nil && !shouldRun() {
			done(ErrCheckpointCanceled)
			return
		}
		done(cp.Checkpoint(destDir))
	})
}

// SetCheckpointVersion makes a completed checkpoint self-describing without
// changing the live database.
func SetCheckpointVersion(db types.StateStore, destDir string, version int64) error {
	setter, ok := db.(types.CheckpointVersionSetter)
	if !ok {
		return fmt.Errorf("state store backend %T cannot set checkpoint version", db)
	}
	return setter.SetCheckpointVersion(destDir, version)
}
