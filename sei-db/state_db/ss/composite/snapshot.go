package composite

import (
	"errors"
	"fmt"
	"sync"
	"sync/atomic"
	"time"

	"github.com/sei-protocol/sei-chain/sei-db/management"
	sssnapshot "github.com/sei-protocol/sei-chain/sei-db/state_db/ss/snapshot"
)

// Online state-store snapshots. Every SnapshotInterval blocks the composite
// store asks each enabled SS member to stage a Pebble checkpoint while the node
// keeps producing blocks. Each member owns its own snapshot root, current link,
// retention, and metadata; composite only chooses the height and coordinates the
// best-effort joint publication.
//
// These snapshots are an input to SS rollback, not an export format. The restore
// model matches SC FlatKV: restore from an SS snapshot, then replay the state WAL
// forward to the target height. State sync imports the SC snapshot stream and
// rebuilds SS from that stream; it does not consume these SS snapshot
// directories.
//
// For every accepted SS snapshot, the label is exact: it is the version the
// write path had just handed to the backends when the snapshot was requested.
// Placing a barrier in each backend's apply queue — rather than sampling what
// the backends had applied — makes that label exact without the request having
// to wait. The barrier orders only the async block-commit queues. Import,
// recovery, pruning, and direct version-marker writes bypass those queues and
// must not call ScheduleSnapshot.
//
// Cross-member pairing is best-effort, not an invariant. Composite stages every
// member and commits only after all members stage successfully, so the normal
// path publishes the same height everywhere. Once members have separate roots,
// however, publication is multiple renames and cannot be atomic across
// directories. Startup therefore does not delete an unpaired height; rollback
// must pick the newest snapshot height present in every required member. In an
// EVM-only process that rule degenerates to the newest EVM snapshot.
//
// Pruning is the one writer nothing orders a snapshot against. A checkpoint can
// capture a partially applied prune — the same state a crash mid-prune leaves on
// the live DB. This is safe for a snapshot because pruning advances each DB's
// earliest marker before deleting history, so the checkpoint never claims a
// range the DB has already dropped. Reopening a snapshot with different member
// floors is allowed; the composite reports the highest floor any member carries.
//
// Managed snapshot directories have no lease because they are not a node-external
// consumption API. Retention may remove any snapshot that rollback does not need.
// If a future tool opens or copies these directories directly, it must first add
// a lease or other hold mechanism.

const (
	SnapshotsDirName    = sssnapshot.SnapshotsDirName
	snapshotCurrentLink = "current"
	snapshotTmpPrefix   = "tmp-"
)

func SnapshotDirName(version int64) string {
	return sssnapshot.SnapshotDirName(version)
}

func ListSnapshotVersions(root string) ([]int64, error) {
	return sssnapshot.ListSnapshotVersions(root)
}

type snapshotMember struct {
	name    string
	manager *sssnapshot.Manager
}

// snapshotCoordinator owns the one-at-a-time cadence for composite mode. It has
// no snapshot root of its own.
type snapshotCoordinator struct {
	interval int64
	minTime  time.Duration
	members  []snapshotMember
	// floor carries the newest height every member holds to the members' own retention, which counts
	// only its own directories and would otherwise let an unpaired newer height crowd it out.
	floor *sssnapshot.Floor

	mu sync.Mutex
	// lastRequested is the newest label already requested or present in any
	// member, so an unpaired height does not get retried under an exact label the
	// write path has already moved past.
	lastRequested int64
	lastRequestAt time.Time
	inFlight      bool
	stopped       bool
	// scheduling closes the gap between accepting a request and enqueueing its
	// barriers. Close waits for it before closing backend queues.
	scheduling sync.WaitGroup
	// publishing tracks the goroutine finishing the accepted snapshot off.
	publishing sync.WaitGroup
}

func newSnapshotCoordinator(
	interval int64,
	minTime time.Duration,
	members []snapshotMember,
	floor *sssnapshot.Floor,
) *snapshotCoordinator {
	c := &snapshotCoordinator{
		interval: interval,
		minTime:  minTime,
		members:  members,
		floor:    floor,
	}
	c.lastRequested = newestMemberSnapshot(members)
	c.lastRequestAt = newestMemberSnapshotModTime(members, c.lastRequested)
	c.recordCommonHeight()
	return c
}

// recordCommonHeight republishes the height every member holds, which is both what retention must keep
// and what an operator watches to see the members drifting apart.
func (c *snapshotCoordinator) recordCommonHeight() {
	common := newestCommonSnapshot(c.members)
	c.floor.Set(common)
	sssnapshot.RecordCommonHeight(common)
}

func (s *CompositeStateStore) startSnapshotManager(members []snapshotMember, floor *sssnapshot.Floor) error {
	if s.config.SnapshotInterval <= 0 {
		return nil
	}
	if len(members) == 0 {
		return errors.New("no state store snapshot members")
	}
	for _, member := range members {
		if member.manager == nil {
			return fmt.Errorf("state store snapshot member %q has no manager", member.name)
		}
	}
	s.snapshotMgr = newSnapshotCoordinator(
		s.config.SnapshotInterval,
		s.config.SnapshotMinTimeInterval,
		members,
		floor,
	)
	logger.Info("state store snapshotting enabled",
		"interval", s.config.SnapshotInterval,
		"minTimeInterval", s.config.SnapshotMinTimeInterval,
		"keepRecent", s.config.SnapshotKeepRecent,
		"members", len(members),
	)
	return nil
}

// stop prevents further snapshots, waits for accepted requests to enqueue their
// barriers, and then waits for active publication. Queued barriers are canceled
// before they start when backend close drains their queues.
func (c *snapshotCoordinator) stop() {
	c.mu.Lock()
	c.stopped = true
	c.mu.Unlock()
	c.scheduling.Wait()
	c.publishing.Wait()
}

func (c *snapshotCoordinator) isRunning() bool {
	c.mu.Lock()
	defer c.mu.Unlock()
	return !c.stopped
}

// maybeSnapshot takes a snapshot when version lands on an interval boundary.
// It is called from the write path for every version, so the common case is the
// modulo test and nothing else.
func (c *snapshotCoordinator) maybeSnapshot(version int64) {
	if c == nil || version <= 0 || c.interval <= 0 || version%c.interval != 0 {
		return
	}
	now := time.Now()
	c.mu.Lock()
	previous := c.lastRequested
	previousRequestAt := c.lastRequestAt
	var skipReason string
	accepted := false
	switch {
	case c.stopped || version <= c.lastRequested:
		// A repeated commit-path call is expected and is not a skipped attempt.
	case c.inFlight:
		skipReason = "in_flight"
	case !c.lastRequestAt.IsZero() && now.Sub(c.lastRequestAt) < c.minTime:
		skipReason = "minimum_time_interval"
	default:
		c.lastRequested = version
		c.lastRequestAt = now
		c.inFlight = true
		c.scheduling.Add(1)
		sssnapshot.RecordInFlight(1)
		accepted = true
	}
	c.mu.Unlock()
	if !accepted {
		if skipReason != "" {
			sssnapshot.RecordSkipped(skipReason)
			// A skipped boundary is the reason a snapshot an operator expected is
			// not on disk, so name the gate rather than leaving only a metric.
			logger.Info("skipping state store snapshot", "version", version, "reason", skipReason)
		}
		return
	}
	defer c.scheduling.Done()
	start := time.Now()
	sssnapshot.RecordAttempt()
	if err := c.requestSnapshot(version, start); err != nil {
		// requestSnapshot only reports an error before it queues anything, so this version is free to
		// be requested again: no barrier is writing under its label, and the write path has enqueued
		// nothing above it yet, which is what keeps a retry within the same block exact.
		sssnapshot.RecordCompletion(start, "failure")
		c.mu.Lock()
		if c.lastRequested == version {
			c.lastRequested = previous
			c.lastRequestAt = previousRequestAt
			c.inFlight = false
			sssnapshot.RecordInFlight(0)
		}
		c.mu.Unlock()
		logger.Error("state store snapshot failed", "version", version, "error", err)
	}
}

func (c *snapshotCoordinator) finishSnapshot() {
	c.mu.Lock()
	c.inFlight = false
	sssnapshot.RecordInFlight(0)
	c.mu.Unlock()
	c.recordCommonHeight()
}

// requestSnapshot asks every member to checkpoint itself into a staging
// directory and publishes the result once they all have.
func (c *snapshotCoordinator) requestSnapshot(version int64, start time.Time) error {
	if len(c.members) == 0 {
		return errors.New("no state store snapshot members")
	}
	// Every member is prepared before any of them is scheduled, so a member that cannot take this
	// version fails the whole request with nothing queued. Half-queued requests are what let a retry
	// of the same version reach a staging directory a barrier from the first attempt still writes to.
	staged := make([]*sssnapshot.Staged, len(c.members))
	for i, member := range c.members {
		if member.manager == nil {
			abortStaged(staged)
			return fmt.Errorf("state store snapshot member %q has no manager", member.name)
		}
		s, err := member.manager.Prepare(version)
		if err != nil {
			abortStaged(staged)
			return fmt.Errorf("prepare %s snapshot: %w", member.name, err)
		}
		staged[i] = s
	}

	var canceled atomic.Bool
	shouldRun := func() bool {
		return c.isRunning() && !canceled.Load()
	}

	var (
		mu        sync.Mutex
		remaining = len(c.members)
		firstErr  error
	)
	for i, member := range c.members {
		member.manager.Schedule(staged[i], shouldRun, func(err error) {
			mu.Lock()
			if err != nil {
				if firstErr == nil {
					firstErr = fmt.Errorf("stage %s snapshot: %w", member.name, err)
				}
				// A snapshot only publishes when every member has it, so a peer still queued has
				// nothing left to produce.
				canceled.Store(true)
			}
			remaining--
			last, outcome := remaining == 0, firstErr
			mu.Unlock()
			if !last {
				return
			}
			c.startPublish(version, staged, outcome, start)
		})
	}
	return nil
}

// startPublish hands a finished set of checkpoints off to a goroutine. It runs
// on whichever backend's apply goroutine finished last, so it must not do the
// work itself: publishing renames directories and prunes old snapshots, and a
// writer stalled on that is a writer not applying blocks.
func (c *snapshotCoordinator) startPublish(
	version int64,
	staged []*sssnapshot.Staged,
	checkpointErr error,
	start time.Time,
) {
	// Taken under the same lock stop uses, so no goroutine is registered after
	// stop has started waiting.
	c.mu.Lock()
	if c.stopped {
		c.mu.Unlock()
		abortStaged(staged)
		sssnapshot.RecordCompletion(start, "canceled")
		c.finishSnapshot()
		return
	}
	c.publishing.Add(1)
	c.mu.Unlock()

	go func() {
		defer c.publishing.Done()
		defer c.finishSnapshot()
		if checkpointErr != nil {
			if errors.Is(checkpointErr, management.ErrCheckpointCanceled) {
				sssnapshot.RecordCompletion(start, "canceled")
			} else {
				sssnapshot.RecordCompletion(start, "failure")
				logger.Error("state store snapshot failed", "version", version, "error", checkpointErr)
			}
			abortStaged(staged)
			return
		}
		for i, member := range c.members {
			if err := member.manager.Commit(staged[i]); err != nil {
				sssnapshot.RecordCompletion(start, "failure")
				logger.Error("failed to publish state store member snapshot",
					"version", version, "member", member.name, "error", err)
				abortStaged(staged[i+1:])
				return
			}
		}
		sssnapshot.RecordCompletion(start, "success")
	}()
}

func abortStaged(staged []*sssnapshot.Staged) {
	for _, s := range staged {
		if s != nil {
			s.Abort()
		}
	}
}

func newestMemberSnapshot(members []snapshotMember) int64 {
	var newest int64
	for _, member := range members {
		if member.manager == nil {
			continue
		}
		newest = max(newest, member.manager.Newest())
	}
	return newest
}

func newestMemberSnapshotModTime(members []snapshotMember, version int64) time.Time {
	var newest time.Time
	for _, member := range members {
		if member.manager == nil {
			continue
		}
		modTime := member.manager.ModTime(version)
		if modTime.After(newest) {
			newest = modTime
		}
	}
	return newest
}

func newestCommonSnapshot(members []snapshotMember) int64 {
	roots := make([]string, 0, len(members))
	for _, member := range members {
		if member.manager == nil {
			return 0
		}
		roots = append(roots, member.manager.Root())
	}
	return sssnapshot.NewestCommonVersion(roots)
}
