// Package controller holds the coordination layer above the DB engines: work
// that decides when an engine-level operation runs, rather than how the engine
// performs it.
package controller

import (
	"maps"
	"sync"
	"time"

	"github.com/sei-protocol/seilog"

	"github.com/sei-protocol/sei-chain/sei-db/config"
)

var checkpointLogger = seilog.NewLogger("db", "checkpoint")

// CheckpointScheduler picks the heights every store checkpoints at, so the stores of one node hold
// checkpoints of the same versions rather than of whatever version each happened to reach.
//
// Stores ask ShouldCheckpoint at every version they commit and call MarkCheckpointComplete once
// that version's checkpoint has finished, whether or not it succeeded: every yes obliges a report.
// A store joins the schedule by asking, and a height is held until every store registered when it
// was picked, along with any that took it afterwards, has reported. A store behind the others is
// handed that same height rather than one that has moved on, and heights arrive no faster than the
// slowest store reaches them.
//
// A no is final and covers every height under it, so a version refused to one store is refused to
// all of them; a yes holds for every store that reaches that height before it is replaced.
//
// A time interval, a block interval, or both may be set. With both set a height has to clear both;
// a value of 0 or less is unused, and with neither set checkpointing is off. Both are measured from
// the last checkpoint, and from the scheduler's creation before there is one.
//
// Every store has to ask at each version it commits. One asking at only some of them never reaches
// the height being held, which stops the node checkpointing rather than only that store.
type CheckpointScheduler struct {
	mu     sync.Mutex
	config config.CheckpointConfig

	// registered holds every store that has asked, which is how a store joins the schedule.
	registered map[string]struct{}
	// awaiting holds the stores the current height is held for that have yet to report it. Empty
	// means no checkpoint is outstanding.
	awaiting map[string]struct{}

	// nextCheckpointVersion is the height stores checkpoint at, 0 before the first one is picked.
	// It stays set once complete, so a store that has yet to reach it is still given that height.
	nextCheckpointVersion int64
	// checkpointedAt is when the last checkpoint completed, the scheduler's creation before the first.
	checkpointedAt time.Time
	// rejectedVersion is the highest height answered no. A later ask at it is answered no again
	// rather than turning yes once an interval elapses under it.
	rejectedVersion int64
}

// NewCheckpointScheduler returns a scheduler holding every store that asks it to one cadence.
func NewCheckpointScheduler(cfg config.CheckpointConfig) *CheckpointScheduler {
	checkpointLogger.Info("checkpoint scheduler created",
		"timeInterval", cfg.TimeInterval, "blockInterval", cfg.BlockInterval)
	return &CheckpointScheduler{
		config:         cfg,
		registered:     make(map[string]struct{}),
		awaiting:       make(map[string]struct{}),
		checkpointedAt: time.Now(),
	}
}

// ShouldCheckpoint reports whether version is a height for store to checkpoint at, registering
// store with the schedule when this is its first ask.
func (s *CheckpointScheduler) ShouldCheckpoint(store string, version int64) bool {
	if !s.checkpointEnabled() || version <= 0 {
		return false
	}

	s.mu.Lock()
	defer s.mu.Unlock()
	s.registered[store] = struct{}{}

	if version == s.nextCheckpointVersion {
		s.holdFor(store)
		return true
	}
	if s.alreadyRejected(version) {
		return false
	}
	if s.checkpointOutstanding() || !s.hasReachedNextInterval(version) {
		s.rejectedVersion = version
		return false
	}
	s.pickCheckpointHeight(version)
	return true
}

// MarkCheckpointComplete records that store has finished the current height. Both intervals start
// once every store the height is held for has reported it. Any other version is ignored, as is a
// repeat from the same store.
//
// A store that took a height must report it on every path out of the checkpoint, a failed one
// included, which in practice means deferring the call. No further height is picked while one store
// is unreported, so a missed call stops the node checkpointing until it restarts.
func (s *CheckpointScheduler) MarkCheckpointComplete(store string, version int64) {
	s.mu.Lock()
	defer s.mu.Unlock()

	if version != s.nextCheckpointVersion {
		return
	}
	if _, outstanding := s.awaiting[store]; !outstanding {
		return
	}
	delete(s.awaiting, store)

	if s.checkpointOutstanding() {
		return
	}
	s.checkpointedAt = time.Now()
	checkpointLogger.Info("checkpoint complete", "version", version)
}

func (s *CheckpointScheduler) checkpointEnabled() bool {
	return s.config.TimeInterval > 0 || s.config.BlockInterval > 0
}

func (s *CheckpointScheduler) checkpointOutstanding() bool {
	return len(s.awaiting) > 0
}

// alreadyRejected reports whether version has been turned down already, either by trailing the
// height in hand or by being asked and refused. Standing by that no is what stops two stores asking
// at different moments from splitting on one version.
func (s *CheckpointScheduler) alreadyRejected(version int64) bool {
	return version < s.nextCheckpointVersion || version <= s.rejectedVersion
}

// hasReachedNextInterval reports whether every configured interval has passed for version.
func (s *CheckpointScheduler) hasReachedNextInterval(version int64) bool {
	timeElapsed := s.config.TimeInterval <= 0 || time.Since(s.checkpointedAt) >= s.config.TimeInterval
	blocksElapsed := s.config.BlockInterval <= 0 || version-s.nextCheckpointVersion >= s.config.BlockInterval
	return timeElapsed && blocksElapsed
}

// pickCheckpointHeight makes version the height in hand, held for the stores registered now. One
// registering later is not held for here, at a version it may already have passed, and is held for
// from the moment it takes a height.
func (s *CheckpointScheduler) pickCheckpointHeight(version int64) {
	s.nextCheckpointVersion = version
	s.awaiting = maps.Clone(s.registered)
}

// holdFor keeps the height in hand from being replaced until store has reported it.
func (s *CheckpointScheduler) holdFor(store string) {
	s.awaiting[store] = struct{}{}
}
