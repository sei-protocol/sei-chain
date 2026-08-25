// Package controller holds the coordination layer above the DB engines: work
// that decides when an engine-level operation runs, rather than how the engine
// performs it.
package controller

import (
	"errors"
	"fmt"
	"sync"

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

// SupportsCheckpoint reports whether db carries every engine capability a scheduled checkpoint needs.
// A store built from several engines answers for the set: one engine short of the capabilities makes
// the whole snapshot unpublishable.
func SupportsCheckpoint(db types.StateStore) bool {
	_, checkpointable := db.(types.Checkpointable)
	_, barrier := db.(types.DrainBarrier)
	_, versionSetter := db.(types.CheckpointVersionSetter)
	return checkpointable && barrier && versionSetter
}

// FanIn returns a report callback for n parallel branches. Each branch calls it once, and the last
// call passes done the first error any branch reported, or nil.
func FanIn(n int, done func(error)) func(error) {
	var (
		mu        sync.Mutex
		remaining = n
		firstErr  error
	)
	return func(err error) {
		mu.Lock()
		if err != nil && firstErr == nil {
			firstErr = err
		}
		remaining--
		isLast := remaining == 0
		// Read under the lock: a peer branch may report between the unlock and the call to done.
		outcome := firstErr
		mu.Unlock()
		if isLast {
			done(outcome)
		}
	}
}

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
