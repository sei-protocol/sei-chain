// Package controller holds the coordination layer above the DB engines: work
// that decides when an engine-level operation runs, rather than how the engine
// performs it.
package controller

import (
	"sync"
	"time"

	"github.com/sei-protocol/seilog"

	"github.com/sei-protocol/sei-chain/sei-db/config"
)

var checkpointLogger = seilog.NewLogger("db", "checkpoint")

// CheckpointScheduler picks the heights every store on a node checkpoints at.
//
// Stores ask ShouldCheckpoint at each live commit and call MarkCheckpointComplete when that
// checkpoint finishes, success or failure. Replay, WAL catch-up, and state-sync must not ask.
// ShouldCheckpoint will register the store.
//
// The goal is that every registered store either checkpoints on the exact same height, or none of them do.
// That is achieved by holding a yes until every registered store has taken the checkpoint on the same height.
// So a lagging store is still told yes at that height, or no if the faster stores rejected that height already.
//
// A store that passes a held height without taking the checkpoint — one whose version jumped over it — is
// released from that height when it asks next time, so it costs that store one checkpoint rather than
// stopping the whole schedule. A store that stops asking altogether could hold the height indefinitely.
//
// A time interval, a block interval, or both may be set. Both set means both must hold; neither disables checkpointing.
type CheckpointScheduler struct {
	mu     sync.Mutex
	config config.CheckpointConfig

	// registered holds every store that has asked, which is how a store joins the schedule.
	registered map[string]struct{}
	// awaiting maps each store nextCheckpointVersion is held for to whether that store has taken
	// the height yet. Empty means every store has checkpointed this height.
	awaiting map[string]bool

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
		awaiting:       make(map[string]bool),
		checkpointedAt: time.Now(),
	}
}

// ShouldCheckpoint reports whether version is a height for store to checkpoint at, registering
// store with the schedule when this is its first ask. Call it only from the live commit path,
// never during replay, WAL catch-up, or state-sync forward-fill.
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
	s.skipPastHeight(store)
	if !(s.allStoresCheckpointed() && s.hasReachedNextInterval(version)) {
		s.rejectedVersion = version
		return false
	}
	s.pickCheckpointHeight(store, version)
	return true
}

// MarkCheckpointComplete records that store has finished the height it was given. Both intervals
// start once every store that height is held for has reported it. Any other version is ignored, as
// is a repeat from the same store.
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
	if _, awaited := s.awaiting[store]; !awaited {
		return
	}
	delete(s.awaiting, store)
	s.updateCheckpointTime()
}

func (s *CheckpointScheduler) checkpointEnabled() bool {
	return s.config.TimeInterval > 0 || s.config.BlockInterval > 0
}

func (s *CheckpointScheduler) allStoresCheckpointed() bool {
	return len(s.awaiting) == 0
}

// alreadyRejected reports whether version has been turned down already, either by trailing
// nextCheckpointVersion or by being asked and refused. Standing by that no is what stops two stores
// asking at different moments from splitting on one version.
func (s *CheckpointScheduler) alreadyRejected(version int64) bool {
	return version < s.nextCheckpointVersion || version <= s.rejectedVersion
}

// hasReachedNextInterval reports whether version satisfies every configured interval.
func (s *CheckpointScheduler) hasReachedNextInterval(version int64) bool {
	timeElapsed := s.config.TimeInterval <= 0 || time.Since(s.checkpointedAt) >= s.config.TimeInterval
	onBlockBoundary := s.config.BlockInterval <= 0 || version%s.config.BlockInterval == 0
	return timeElapsed && onBlockBoundary
}

// pickCheckpointHeight sets nextCheckpointVersion to version and holds it for the stores registered
// now, of which store is the one that has taken it. One registering later is not held for here, at
// a version it may already have passed, and is held for from the moment it takes a height.
func (s *CheckpointScheduler) pickCheckpointHeight(store string, version int64) {
	s.nextCheckpointVersion = version
	s.awaiting = make(map[string]bool, len(s.registered))
	for name := range s.registered {
		s.awaiting[name] = false
	}
	s.awaiting[store] = true
}

// holdFor records that store has taken nextCheckpointVersion, which is held until store reports it.
func (s *CheckpointScheduler) holdFor(store string) {
	s.awaiting[store] = true
}

// skipPastHeight releases the hold store has on nextCheckpointVersion, for a store asking above
// that height and so past it for good. A store that took the height keeps its hold: it may be
// writing that checkpoint while it commits later versions, and the intervals wait for it.
//
// Callers must have ruled out versions at or under nextCheckpointVersion, which is what makes an
// ask proof that the store has passed it.
func (s *CheckpointScheduler) skipPastHeight(store string) {
	if took, awaited := s.awaiting[store]; !awaited || took {
		return
	}
	delete(s.awaiting, store)
	s.updateCheckpointTime()
}

// updateCheckpointTime records when nextCheckpointVersion completed, once every store has
// checkpointed this height.
func (s *CheckpointScheduler) updateCheckpointTime() {
	if s.allStoresCheckpointed() {
		s.checkpointedAt = time.Now()
	}
}
