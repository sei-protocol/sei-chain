// Package controller holds the coordination layer above the DB engines: work
// that decides when an engine-level operation runs, rather than how the engine
// performs it.
package controller

import (
	"context"
	"errors"
	"fmt"
	"maps"
	"slices"
	"strings"
	"sync"
	"time"

	"github.com/sei-protocol/seilog"
)

var checkpointLogger = seilog.NewLogger("db", "checkpoint")

// checkpointPollInterval is how often the scheduler looks for a boundary to dispatch. It has to stay
// well inside the time the stores take to cover an interval's worth of versions, or boundaries pass
// undispatched.
const checkpointPollInterval = 10 * time.Second

// CheckpointConfig is the cadence a CheckpointScheduler holds every registered store to.
type CheckpointConfig struct {
	// CheckpointInterval is how many blocks apart checkpoints are taken. 0 turns checkpointing off.
	CheckpointInterval int64

	// MinTimeBetweenCheckpoints is the shortest wall-clock gap allowed between one checkpoint
	// finishing and the next being scheduled, which bounds how fast a node replaying blocks
	// checkpoints. 0 leaves CheckpointInterval as the only pacing.
	MinTimeBetweenCheckpoints time.Duration
}

// Validate reports whether this config describes a cadence that can be scheduled.
func (c CheckpointConfig) Validate() error {
	if c.CheckpointInterval < 0 {
		return fmt.Errorf("checkpoint interval must not be negative, got %d", c.CheckpointInterval)
	}
	if c.MinTimeBetweenCheckpoints < 0 {
		return fmt.Errorf("minimum time between checkpoints must not be negative, got %s",
			c.MinTimeBetweenCheckpoints)
	}
	return nil
}

// CheckpointScheduler drives one checkpoint cadence across every store registered with it, so that a
// node's stores hold checkpoints of the same versions rather than of whatever version each happened to
// be at. Each cycle picks the next interval boundary above every store's committed version and hands
// that one version to all of them, to checkpoint when their own write paths reach it; one target is
// outstanding at a time.
//
// Start and Close are the owner's to call, from one goroutine: nothing here is guarded.
type CheckpointScheduler struct {
	config CheckpointConfig
	ctx    context.Context
	stopCh chan struct{}
	wg     sync.WaitGroup

	stores  map[string]CheckpointableStore
	started bool
	closed  bool

	// Only the run loop touches these, through scheduleNextCheckpoint.
	scheduledVersion int64
	lastCheckpointAt time.Time
}

// NewCheckpointScheduler returns a scheduler that holds every store in stores to config, or that
// schedules nothing when config turns checkpointing off. Call Start to begin.
func NewCheckpointScheduler(
	ctx context.Context,
	config CheckpointConfig,
	stores map[string]CheckpointableStore,
) (*CheckpointScheduler, error) {
	if ctx == nil {
		return nil, errors.New("context is required")
	}
	if err := config.Validate(); err != nil {
		return nil, err
	}
	copied := make(map[string]CheckpointableStore, len(stores))
	for name, store := range stores {
		if name == "" {
			return nil, errors.New("checkpoint store name is required")
		}
		if store == nil {
			return nil, fmt.Errorf("checkpoint store %q is nil", name)
		}
		copied[name] = store
	}
	return &CheckpointScheduler{
		config: config,
		ctx:    ctx,
		stopCh: make(chan struct{}),
		stores: copied,
	}, nil
}

// Start begins dispatching targets until Close is called or ctx is cancelled. Starting twice is an
// error.
func (s *CheckpointScheduler) Start() error {
	if s.closed {
		return errors.New("cannot start a closed checkpoint scheduler")
	}
	if s.started {
		return errors.New("checkpoint scheduler already started")
	}
	s.started = true
	checkpointLogger.Info("checkpoint scheduler started",
		"interval", s.config.CheckpointInterval,
		"minTimeBetweenCheckpoints", s.config.MinTimeBetweenCheckpoints,
		"stores", strings.Join(slices.Sorted(maps.Keys(s.stores)), ","),
	)
	s.wg.Add(1)
	go s.run()
	return nil
}

// Close stops dispatching and waits for the run loop to exit.
func (s *CheckpointScheduler) Close() error {
	if s.closed {
		return nil
	}
	s.closed = true
	close(s.stopCh)
	s.wg.Wait()
	return nil
}

// CheckpointInProgress reports whether any registered store is still writing a checkpoint.
func (s *CheckpointScheduler) CheckpointInProgress() bool {
	for _, store := range s.stores {
		if store.CheckpointInProgress() {
			return true
		}
	}
	return false
}

func (s *CheckpointScheduler) run() {
	defer s.wg.Done()

	if s.config.CheckpointInterval == 0 || len(s.stores) == 0 {
		return
	}

	ticker := time.NewTicker(checkpointPollInterval)
	defer ticker.Stop()

	for {
		// Ahead of the first wait, not after it: there is already a boundary to announce by the time
		// Start returns, and waiting out a poll interval only delays the first checkpoint.
		s.scheduleNextCheckpoint()

		select {
		case <-s.stopCh:
			return
		case <-s.ctx.Done():
			return
		case <-ticker.C:
		}
	}
}

// scheduleNextCheckpoint runs one cycle: it hands the next boundary to every store, or does nothing
// when a store is still writing the last checkpoint, has not reached its scheduled version, or the
// last one finished too recently.
func (s *CheckpointScheduler) scheduleNextCheckpoint() {
	if s.CheckpointInProgress() {
		return
	}
	if s.scheduledVersion != 0 && !s.allReached(s.scheduledVersion) {
		return
	}
	s.noteCheckpointFinished()
	if s.withinMinTime() {
		return
	}

	targetVersion := nextCheckpointVersion(s.stores, s.config.CheckpointInterval)
	for _, store := range s.stores {
		store.ScheduleCheckpoint(targetVersion)
	}
	s.scheduledVersion = targetVersion
	checkpointLogger.Info("checkpoint scheduled",
		"targetVersion", targetVersion, "stores", strings.Join(slices.Sorted(maps.Keys(s.stores)), ","))
}

// allReached reports whether every store has committed at least version.
func (s *CheckpointScheduler) allReached(version int64) bool {
	for _, store := range s.stores {
		if store.LatestVersion() < version {
			return false
		}
	}
	return true
}

// noteCheckpointFinished records that the last scheduled checkpoint completed, starting the
// minimum-time gate. scheduledVersion is what marks this as the completion: later cycles also find
// no store writing, and treating those as completions too would push the gate forward every poll.
func (s *CheckpointScheduler) noteCheckpointFinished() {
	if s.scheduledVersion == 0 {
		return
	}
	s.scheduledVersion = 0
	s.lastCheckpointAt = time.Now()
}

// withinMinTime reports whether the last checkpoint finished too recently for another to be scheduled.
//
// Timed from the checkpoint finishing rather than from its dispatch: dispatch runs an interval of
// blocks ahead, so timing from there would leave the gap short by however long the stores took.
func (s *CheckpointScheduler) withinMinTime() bool {
	if s.config.MinTimeBetweenCheckpoints == 0 || s.lastCheckpointAt.IsZero() {
		return false
	}
	return time.Since(s.lastCheckpointAt) < s.config.MinTimeBetweenCheckpoints
}

// nextCheckpointVersion returns the next interval-aligned height strictly above every store's latest version.
func nextCheckpointVersion(stores map[string]CheckpointableStore, interval int64) int64 {
	var latest int64
	for _, store := range stores {
		latest = max(latest, store.LatestVersion())
	}
	return (latest/interval + 1) * interval
}
