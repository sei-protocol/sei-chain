package evm

import (
	"errors"
	"sync"
	"sync/atomic"

	"github.com/sei-protocol/sei-chain/sei-db/controller"
	sssnapshot "github.com/sei-protocol/sei-chain/sei-db/state_db/ss/snapshot"
)

// Online snapshots of the EVM state store, for a node that drives SS directly rather than through
// the composite store.
//
// The composite store picks its own heights off an interval and coordinates several members. Here
// there is one member and the heights come from a controller.CheckpointScheduler shared with every
// other store on the node, so a restore has one height to resolve rather than a per-store cadence to
// reconcile.
//
// These snapshots are an input to SS rollback, not an export format: a restore starts from one and
// replays the state WAL forward to the target height.

// checkpointState holds what a snapshot in flight needs to outlive the commit that requested it.
type checkpointState struct {
	// mu guards scheduler, stopped and lastOffered.
	mu        sync.Mutex
	scheduler *controller.CheckpointScheduler
	stopped   bool

	// lastOffered is the newest version handed to the schedule. The commit path may revisit a
	// version, and a second offer of one would stage a checkpoint the first is already writing.
	lastOffered int64

	// publishing tracks the goroutines finishing accepted snapshots off. Close waits for them,
	// because publishing reads and stamps the databases Close is about to shut.
	publishing sync.WaitGroup
}

// SetCheckpointScheduler hands this store the schedule it takes its checkpoint heights from. Until it
// has one, CommitBlock takes no snapshot and the store snapshots nothing.
//
// A nil scheduler stands the store's snapshotting down. Safe to call on an open store.
func (s *EVMStateStore) SetCheckpointScheduler(scheduler *controller.CheckpointScheduler) {
	s.checkpoint.mu.Lock()
	defer s.checkpoint.mu.Unlock()
	s.checkpoint.scheduler = scheduler
}

// scheduleSnapshot offers version to the checkpoint schedule and takes a snapshot when the schedule
// picks it. It returns as soon as the checkpoint is queued, without waiting for it to be written.
//
// CommitBlock reaches it once version has been enqueued on every managed database and before any
// later one is: that is what makes the snapshot's label exact, since each database's checkpoint
// barrier then lands after this version and before the next. Nothing else offers a version, since
// the apply methods are shared with import, recovery, prune and the benchmark harness.
func (s *EVMStateStore) scheduleSnapshot(version int64) {
	scheduler := s.acceptCheckpointOffer(version)
	if scheduler == nil {
		return
	}
	if !scheduler.ShouldCheckpoint(prunableStoreName, version) {
		return
	}
	// From here the schedule holds this height until it is reported, so every path out has to report
	// it. An unreported height stops the whole node checkpointing until it restarts.
	if err := s.startCheckpoint(scheduler, version); err != nil {
		logger.Error("failed to start EVM state store snapshot", "version", version, "err", err)
		scheduler.MarkCheckpointComplete(prunableStoreName, version)
	}
}

// startCheckpoint queues the checkpoint for version and arranges for it to be published, and for the
// schedule to be told, once it is written. An error means nothing was queued.
func (s *EVMStateStore) startCheckpoint(scheduler *controller.CheckpointScheduler, version int64) error {
	staged, err := s.snapshotMgr.Prepare(version)
	if err != nil {
		return err
	}

	// Registered before the checkpoint is queued, so Close cannot start waiting after this snapshot
	// has been accepted but before it has a goroutine to wait for.
	s.checkpoint.mu.Lock()
	if s.checkpoint.stopped {
		s.checkpoint.mu.Unlock()
		staged.Abort()
		return errors.New("EVM state store is closing")
	}
	s.checkpoint.publishing.Add(1)
	s.checkpoint.mu.Unlock()

	var canceled atomic.Bool
	shouldRun := func() bool {
		return s.checkpointRunning() && !canceled.Load()
	}
	s.snapshotMgr.Schedule(staged, shouldRun, func(checkpointErr error) {
		if checkpointErr != nil {
			canceled.Store(true)
		}
		// Handed to a goroutine because this runs on a database's own apply goroutine: publishing
		// renames directories and prunes old snapshots, and a writer stalled on that is a writer not
		// applying blocks.
		go func() {
			defer s.checkpoint.publishing.Done()
			defer scheduler.MarkCheckpointComplete(prunableStoreName, version)
			s.publishCheckpoint(staged, version, checkpointErr)
		}()
	})
	return nil
}

// publishCheckpoint makes a written checkpoint the current snapshot, or discards it when the
// checkpoint failed or was canceled.
func (s *EVMStateStore) publishCheckpoint(staged *sssnapshot.Staged, version int64, checkpointErr error) {
	if checkpointErr != nil {
		if !errors.Is(checkpointErr, sssnapshot.ErrCheckpointCanceled) {
			logger.Error("EVM state store snapshot failed", "version", version, "err", checkpointErr)
		}
		staged.Abort()
		return
	}
	if err := s.snapshotMgr.Commit(staged); err != nil {
		logger.Error("failed to publish EVM state store snapshot", "version", version, "err", err)
	}
}

// acceptCheckpointOffer returns the schedule in force, or nil when this store is not checkpointing
// or has been offered version already.
func (s *EVMStateStore) acceptCheckpointOffer(version int64) *controller.CheckpointScheduler {
	if s.snapshotMgr == nil || version <= 0 {
		return nil
	}
	s.checkpoint.mu.Lock()
	defer s.checkpoint.mu.Unlock()
	if s.checkpoint.scheduler == nil || version <= s.checkpoint.lastOffered {
		return nil
	}
	s.checkpoint.lastOffered = version
	return s.checkpoint.scheduler
}

// checkpointRunning reports whether a queued checkpoint should still run.
func (s *EVMStateStore) checkpointRunning() bool {
	s.checkpoint.mu.Lock()
	defer s.checkpoint.mu.Unlock()
	return !s.checkpoint.stopped
}

// stopCheckpoints refuses further snapshots and waits for the accepted ones to finish publishing.
// Queued checkpoints that have not started are canceled when closing the databases drains their
// queues.
func (s *EVMStateStore) stopCheckpoints() {
	s.checkpoint.mu.Lock()
	s.checkpoint.stopped = true
	s.checkpoint.mu.Unlock()
	s.checkpoint.publishing.Wait()
}

// quiesceCheckpoints refuses further snapshots and waits for the accepted ones to finish publishing,
// returning the call that lets them resume. A publish in flight reads and stamps the databases, so it
// has to finish before anything closes or replaces them.
//
// It is stopCheckpoints for an operation the store outlives. A store already stopped stays stopped.
func (s *EVMStateStore) quiesceCheckpoints() (resume func()) {
	s.checkpoint.mu.Lock()
	stopped := s.checkpoint.stopped
	s.checkpoint.stopped = true
	s.checkpoint.mu.Unlock()
	s.checkpoint.publishing.Wait()

	return func() {
		s.checkpoint.mu.Lock()
		defer s.checkpoint.mu.Unlock()
		s.checkpoint.stopped = stopped
	}
}

// rewindLastOffered drops the newest version handed to the schedule to version, so a height at or
// below the one a rewind landed on can be offered again.
//
// Without it a rewind stands the store's snapshotting down until it climbs back past the height it
// rewound from, since a version at or below lastOffered is refused as a repeat offer.
func (s *EVMStateStore) rewindLastOffered(version int64) {
	s.checkpoint.mu.Lock()
	defer s.checkpoint.mu.Unlock()
	if version < s.checkpoint.lastOffered {
		s.checkpoint.lastOffered = version
	}
}
