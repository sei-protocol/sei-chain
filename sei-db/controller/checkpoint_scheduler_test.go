package controller

import (
	"context"
	"slices"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

// Which target the scheduler picks, and whether it picks one at all, is decided synchronously in
// scheduleNextCheckpoint. Those tests drive it directly and assert exactly; only the tests about the run
// loop itself — that it dispatches, that it stops — start a goroutine.

// fakeStore stands in for a store the scheduler drives. It records every target it is offered so a
// test can assert what the stores were asked for, which is the scheduler's whole output.
type fakeStore struct {
	mu      sync.Mutex
	version int64
	pending int64
	running bool
	offered []int64
}

func newFakeStore(version int64) *fakeStore {
	return &fakeStore{version: version}
}

func (f *fakeStore) ScheduleCheckpoint(targetVersion int64) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.offered = append(f.offered, targetVersion)
	f.pending = targetVersion
}

func (f *fakeStore) LatestVersion() int64 {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.version
}

func (f *fakeStore) CheckpointInProgress() bool {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.running
}

func (f *fakeStore) setRunning(running bool) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.running = running
}

// commitTo advances the store to version, performing an accepted checkpoint on the way past it.
func (f *fakeStore) commitTo(version int64) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.version = version
	if f.pending != 0 && version >= f.pending {
		f.pending = 0
	}
}

func (f *fakeStore) offeredTargets() []int64 {
	f.mu.Lock()
	defer f.mu.Unlock()
	return slices.Clone(f.offered)
}

// newScheduler returns an unstarted scheduler over stores.
func newScheduler(t *testing.T, interval int64, stores map[string]*fakeStore) *CheckpointScheduler {
	t.Helper()
	return newConfiguredScheduler(t, CheckpointConfig{CheckpointInterval: interval}, stores)
}

// newConfiguredScheduler is newScheduler for the tests that care about more of the cadence than the
// block interval.
func newConfiguredScheduler(
	t *testing.T,
	config CheckpointConfig,
	stores map[string]*fakeStore,
) *CheckpointScheduler {
	t.Helper()
	copied := make(map[string]CheckpointableStore, len(stores))
	for name, store := range stores {
		copied[name] = store
	}
	scheduler, err := NewCheckpointScheduler(context.Background(), config, copied)
	require.NoError(t, err)
	return scheduler
}

// requireLoopStopped waits, with a bound, for the run loop to have exited — or to never have started.
// The wait is bounded rather than a bare wg.Wait because the failure it looks for is a loop that keeps
// running, and that failure has to be a test failure rather than a test that hangs.
func requireLoopStopped(t *testing.T, scheduler *CheckpointScheduler) {
	t.Helper()
	stopped := make(chan struct{})
	go func() {
		scheduler.wg.Wait()
		close(stopped)
	}()
	select {
	case <-stopped:
	case <-time.After(time.Second):
		t.Fatal("checkpoint scheduler run loop is still running")
	}
}

// ---------------------------------------------------------------------------
// Which target a cycle picks
// ---------------------------------------------------------------------------

func TestNextCheckpointVersionAlignsToInterval(t *testing.T) {
	for _, tc := range []struct {
		latest, interval, want int64
	}{
		{latest: 0, interval: 1000, want: 1000},
		{latest: 1, interval: 1000, want: 1000},
		{latest: 999, interval: 1000, want: 1000},
		{latest: 1000, interval: 1000, want: 2000},
		{latest: 2100, interval: 1000, want: 3000},
		{latest: 3700, interval: 1000, want: 4000},
		{latest: 4000, interval: 1000, want: 5000},
	} {
		stores := map[string]CheckpointableStore{"only": newFakeStore(tc.latest)}
		require.Equal(t, tc.want, nextCheckpointVersion(stores, tc.interval),
			"latest %d interval %d", tc.latest, tc.interval)
	}
}

// The point of the scheduler: every store is asked for the same version, even when they are at
// different versions when the target is chosen.
func TestCycleOffersOneTargetToEveryStore(t *testing.T) {
	fast, slow := newFakeStore(95), newFakeStore(80)
	scheduler := newScheduler(t, 100, map[string]*fakeStore{"fast": fast, "slow": slow})

	scheduler.scheduleNextCheckpoint()

	require.Equal(t, []int64{100}, fast.offeredTargets())
	require.Equal(t, []int64{100}, slow.offeredTargets())
}

// The target has to be above every store's version, not above the laggard's: a store that is ahead can
// no longer checkpoint at a version it has passed, and would answer with a different one.
func TestTargetIsAboveTheMostAdvancedStore(t *testing.T) {
	ahead, behind := newFakeStore(250), newFakeStore(10)
	scheduler := newScheduler(t, 100, map[string]*fakeStore{"ahead": ahead, "behind": behind})

	scheduler.scheduleNextCheckpoint()

	require.Equal(t, []int64{300}, ahead.offeredTargets())
	require.Equal(t, []int64{300}, behind.offeredTargets())
}

// A target is offered well before the stores reach it, which is what makes the shared version an
// invariant rather than a race. Nothing about the current version gates the offer.
func TestTargetIsOfferedAWholeIntervalAhead(t *testing.T) {
	store := newFakeStore(1)
	scheduler := newScheduler(t, 1000, map[string]*fakeStore{"only": store})

	scheduler.scheduleNextCheckpoint()

	require.Equal(t, []int64{1000}, store.offeredTargets(), "offered at version 1, 999 blocks early")
}

// One target is outstanding at a time, so a store that has not committed past its target holds the
// next boundary back rather than collecting targets it would service late.
func TestNoNewTargetWhileOneIsOutstanding(t *testing.T) {
	prompt, lagging := newFakeStore(0), newFakeStore(0)
	scheduler := newScheduler(t, 10, map[string]*fakeStore{"prompt": prompt, "lagging": lagging})

	scheduler.scheduleNextCheckpoint()
	require.Equal(t, []int64{10}, prompt.offeredTargets())
	prompt.commitTo(35)

	// The laggard still holds target 10, so the boundaries at 20 and 30 pass without an offer.
	scheduler.scheduleNextCheckpoint()
	scheduler.scheduleNextCheckpoint()
	require.Equal(t, []int64{10}, prompt.offeredTargets())

	lagging.commitTo(11)
	scheduler.scheduleNextCheckpoint()
	require.Equal(t, []int64{10, 40}, prompt.offeredTargets())
	require.Equal(t, []int64{10, 40}, lagging.offeredTargets())
}

// Stores bump LatestVersion on commit and only then start the checkpoint write. A poll in that gap
// sees the scheduled version reached and CheckpointInProgress still false; that is not completion.
func TestNoNewTargetWhileLatestVersionIsStillTheScheduledVersion(t *testing.T) {
	store := newFakeStore(0)
	scheduler := newScheduler(t, 10, map[string]*fakeStore{"only": store})

	scheduler.scheduleNextCheckpoint()
	store.commitTo(10)
	scheduler.scheduleNextCheckpoint()
	require.Equal(t, []int64{10}, store.offeredTargets(),
		"LatestVersion at the scheduled height is the commit, not a finished checkpoint")

	store.commitTo(11)
	scheduler.scheduleNextCheckpoint()
	require.Equal(t, []int64{10, 20}, store.offeredTargets())
}

// A store still writing its snapshot holds the next boundary even after it has committed past the height.
func TestNoNewTargetWhileAStoreIsWriting(t *testing.T) {
	store := newFakeStore(0)
	scheduler := newScheduler(t, 10, map[string]*fakeStore{"only": store})

	scheduler.scheduleNextCheckpoint()
	store.commitTo(15)
	store.setRunning(true)
	scheduler.scheduleNextCheckpoint()
	require.Equal(t, []int64{10}, store.offeredTargets())

	store.setRunning(false)
	scheduler.scheduleNextCheckpoint()
	require.Equal(t, []int64{10, 20}, store.offeredTargets())
}

// CheckpointInProgress is an aggregate over the stores: true while any store is writing.
func TestCheckpointInProgressReportsAnyStore(t *testing.T) {
	first, second := newFakeStore(0), newFakeStore(0)
	scheduler := newScheduler(t, 10, map[string]*fakeStore{"first": first, "second": second})

	require.False(t, scheduler.CheckpointInProgress())
	first.setRunning(true)
	second.setRunning(true)
	require.True(t, scheduler.CheckpointInProgress())

	first.setRunning(false)
	require.True(t, scheduler.CheckpointInProgress(), "second store is still writing")
	second.setRunning(false)
	require.False(t, scheduler.CheckpointInProgress())
}

// ---------------------------------------------------------------------------
// The minimum-time gate
// ---------------------------------------------------------------------------

// pacedScheduler returns a scheduler whose minimum-time gate is wide enough that no test elapses it by
// running. Tests that need it elapsed move lastCheckpointAt rather than waiting.
func pacedScheduler(t *testing.T, stores map[string]*fakeStore) *CheckpointScheduler {
	t.Helper()
	return newConfiguredScheduler(t, CheckpointConfig{
		CheckpointInterval:        10,
		MinTimeBetweenCheckpoints: time.Hour,
	}, stores)
}

// Nothing has been checkpointed yet, so there is no gap to enforce and the first boundary is not held.
func TestTheMinTimeGateDoesNotDelayTheFirstCheckpoint(t *testing.T) {
	store := newFakeStore(0)
	scheduler := pacedScheduler(t, map[string]*fakeStore{"only": store})

	scheduler.scheduleNextCheckpoint()

	require.Equal(t, []int64{10}, store.offeredTargets())
}

// Once a checkpoint has been taken, the next boundary waits for the gate even though the stores have
// long since passed it.
func TestTheMinTimeGateHoldsBackTheNextTarget(t *testing.T) {
	store := newFakeStore(0)
	scheduler := pacedScheduler(t, map[string]*fakeStore{"only": store})

	scheduler.scheduleNextCheckpoint()
	require.Equal(t, []int64{10}, store.offeredTargets())
	store.commitTo(100)

	scheduler.scheduleNextCheckpoint()
	scheduler.scheduleNextCheckpoint()

	require.Equal(t, []int64{10}, store.offeredTargets(), "the gate has not elapsed")
}

func TestTheMinTimeGateReleasesOnceItElapses(t *testing.T) {
	store := newFakeStore(0)
	scheduler := pacedScheduler(t, map[string]*fakeStore{"only": store})

	scheduler.scheduleNextCheckpoint()
	store.commitTo(11)
	scheduler.scheduleNextCheckpoint()
	require.Equal(t, []int64{10}, store.offeredTargets())

	scheduler.lastCheckpointAt = time.Now().Add(-2 * time.Hour)
	scheduler.scheduleNextCheckpoint()

	require.Equal(t, []int64{10, 20}, store.offeredTargets())
}

// The gate runs from the checkpoint finishing, not from its target being dispatched. A store that is
// slow to reach its target would otherwise spend the gate's window getting there, and the next
// checkpoint would follow it by less than the configured gap.
func TestTheMinTimeGateIsTimedFromCompletion(t *testing.T) {
	store := newFakeStore(0)
	scheduler := pacedScheduler(t, map[string]*fakeStore{"only": store})

	scheduler.scheduleNextCheckpoint()
	require.True(t, scheduler.lastCheckpointAt.IsZero(), "dispatching a target does not start the gate")

	store.commitTo(11)
	scheduler.scheduleNextCheckpoint()
	require.False(t, scheduler.lastCheckpointAt.IsZero(), "the finished checkpoint starts the gate")
}

func TestAZeroMinTimeLeavesTheBlockIntervalAsTheOnlyPacing(t *testing.T) {
	store := newFakeStore(0)
	scheduler := newScheduler(t, 10, map[string]*fakeStore{"only": store})

	scheduler.scheduleNextCheckpoint()
	store.commitTo(11)
	scheduler.scheduleNextCheckpoint()

	require.Equal(t, []int64{10, 20}, store.offeredTargets())
}

// ---------------------------------------------------------------------------
// Lifecycle
// ---------------------------------------------------------------------------

// The run loop dispatches on its own, which is the one thing the direct-cycle tests above cannot show.
//
// The bound is well inside checkpointPollInterval on purpose: it holds the loop to running its first
// cycle at Start, rather than after sitting out a poll interval first.
func TestStartedSchedulerDispatchesOnItsOwn(t *testing.T) {
	store := newFakeStore(0)
	scheduler := newScheduler(t, 10, map[string]*fakeStore{"only": store})
	require.NoError(t, scheduler.Start())
	t.Cleanup(func() { require.NoError(t, scheduler.Close()) })

	require.Eventually(t, func() bool {
		return slices.Equal(store.offeredTargets(), []int64{10})
	}, 100*time.Millisecond, time.Millisecond, "the run loop never offered a target")
}

func TestNewRejectsAnEmptyStoreName(t *testing.T) {
	_, err := NewCheckpointScheduler(context.Background(), CheckpointConfig{CheckpointInterval: 10},
		map[string]CheckpointableStore{"": newFakeStore(0)})
	require.ErrorContains(t, err, "store name is required")
}

func TestNewRejectsANilStore(t *testing.T) {
	_, err := NewCheckpointScheduler(context.Background(), CheckpointConfig{CheckpointInterval: 10},
		map[string]CheckpointableStore{"ss": nil})
	require.ErrorContains(t, err, "is nil")
}

func TestNewRejectsANegativeInterval(t *testing.T) {
	_, err := NewCheckpointScheduler(context.Background(), CheckpointConfig{CheckpointInterval: -1}, nil)
	require.ErrorContains(t, err, "interval must not be negative")
}

func TestNewRejectsANegativeMinTime(t *testing.T) {
	_, err := NewCheckpointScheduler(context.Background(), CheckpointConfig{
		CheckpointInterval:        10,
		MinTimeBetweenCheckpoints: -time.Second,
	}, nil)
	require.ErrorContains(t, err, "minimum time between checkpoints must not be negative")
}

// A scheduler with no stores has nobody to checkpoint, so run returns without a loop.
func TestAnEmptyStoreSetRunsNoLoop(t *testing.T) {
	scheduler := newScheduler(t, 10, nil)

	require.NoError(t, scheduler.Start())
	requireLoopStopped(t, scheduler)
	require.False(t, scheduler.CheckpointInProgress())
	require.NoError(t, scheduler.Close())
}

// Interval 0 turns checkpointing off. run returns without a loop so a cycle never divides by zero.
func TestAZeroIntervalRunsNoLoop(t *testing.T) {
	store := newFakeStore(0)
	scheduler := newScheduler(t, 0, map[string]*fakeStore{"only": store})

	require.NoError(t, scheduler.Start())
	requireLoopStopped(t, scheduler)
	require.Empty(t, store.offeredTargets())
	require.False(t, scheduler.CheckpointInProgress())
	require.NoError(t, scheduler.Close())
}

func TestCloseIsIdempotentAndSafeBeforeStart(t *testing.T) {
	scheduler := newScheduler(t, 10, nil)

	require.NoError(t, scheduler.Close())
	require.NoError(t, scheduler.Close())
	require.ErrorContains(t, scheduler.Start(), "closed")
}

func TestCloseStopsTheRunLoop(t *testing.T) {
	scheduler := newScheduler(t, 10, map[string]*fakeStore{"only": newFakeStore(0)})
	require.NoError(t, scheduler.Start())

	require.NoError(t, scheduler.Close())

	requireLoopStopped(t, scheduler)
}

// Cancelling the context ends the run loop. Asserted as the loop exiting rather than as an absence of
// further offers: the loop selects over the ticker and the cancellation together, so a cycle already
// runnable at the moment of cancellation may still run, and a test forbidding that fails a few runs in
// a thousand.
func TestCancellingTheContextStopsTheRunLoop(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	scheduler, err := NewCheckpointScheduler(ctx, CheckpointConfig{CheckpointInterval: 10},
		map[string]CheckpointableStore{"only": newFakeStore(0)})
	require.NoError(t, err)
	require.NoError(t, scheduler.Start())

	cancel()

	requireLoopStopped(t, scheduler)
	require.NoError(t, scheduler.Close())
}
