package controller

import (
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/sei-protocol/sei-chain/sei-db/config"
)

// newScheduler spells the two intervals out positionally, which reads better than a config literal
// in tests that vary nothing else.
func newScheduler(timeInterval time.Duration, blockInterval int64) *CheckpointScheduler {
	return NewCheckpointScheduler(config.CheckpointConfig{
		TimeInterval:  timeInterval,
		BlockInterval: blockInterval,
	})
}

// elapseTimeInterval moves the interval's starting point back rather than waiting it out.
func (s *CheckpointScheduler) elapseTimeInterval() {
	s.checkpointedAt = time.Now().Add(-s.config.TimeInterval)
}

// ---------------------------------------------------------------------------
// Which intervals are in use
// ---------------------------------------------------------------------------

func TestNeitherIntervalSetDisablesCheckpointing(t *testing.T) {
	for _, scheduler := range []*CheckpointScheduler{
		newScheduler(0, 0),
		newScheduler(-time.Second, 0),
		newScheduler(0, -1),
	} {
		require.False(t, scheduler.ShouldCheckpoint("sc", 1))
		require.False(t, scheduler.ShouldCheckpoint("sc", 1_000_000))
	}
}

func TestNonPositiveVersionsAreNeverCheckpointHeights(t *testing.T) {
	scheduler := newScheduler(time.Hour, 10)
	scheduler.elapseTimeInterval()

	require.False(t, scheduler.ShouldCheckpoint("sc", 0))
	require.False(t, scheduler.ShouldCheckpoint("sc", -1))
}

// The first height clears the same intervals as every later one, measured from the scheduler's
// creation. Answering yes to whichever version happens to ask first would ignore the cadence.
func TestTheFirstHeightWaitsForTheTimeInterval(t *testing.T) {
	scheduler := newScheduler(time.Hour, 0)

	require.False(t, scheduler.ShouldCheckpoint("sc", 10))

	scheduler.elapseTimeInterval()
	require.True(t, scheduler.ShouldCheckpoint("sc", 11))
}

func TestTheFirstHeightWaitsForTheBlockInterval(t *testing.T) {
	scheduler := newScheduler(0, 100)

	require.False(t, scheduler.ShouldCheckpoint("sc", 99))
	require.True(t, scheduler.ShouldCheckpoint("sc", 100))
}

// Heights sit on multiples of the block interval rather than a count from the last one, so a node
// that starts mid-grid still checkpoints at the same heights as one that did not.
func TestBlockIntervalHeightsAreMultiplesOfTheInterval(t *testing.T) {
	scheduler := newScheduler(0, 100)

	for _, offGrid := range []int64{4001, 4050, 4099} {
		require.False(t, scheduler.ShouldCheckpoint("sc", offGrid))
	}
	require.True(t, scheduler.ShouldCheckpoint("sc", 4100))
}

// With both set a height has to clear both, so the tighter one paces the cadence.
func TestBothIntervalsMustElapseWhenBothAreSet(t *testing.T) {
	scheduler := newScheduler(time.Hour, 10)
	scheduler.elapseTimeInterval()
	require.True(t, scheduler.ShouldCheckpoint("sc", 10))
	scheduler.MarkCheckpointComplete("sc", 10)

	require.False(t, scheduler.ShouldCheckpoint("sc", 15), "neither interval has elapsed")

	scheduler.elapseTimeInterval()
	require.False(t, scheduler.ShouldCheckpoint("sc", 19), "the block interval has not elapsed")
	require.True(t, scheduler.ShouldCheckpoint("sc", 20))
}

// ---------------------------------------------------------------------------
// One answer per height
// ---------------------------------------------------------------------------

func TestAPickedHeightIsYesForEveryStore(t *testing.T) {
	scheduler := newScheduler(0, 10)
	require.True(t, scheduler.ShouldCheckpoint("sc", 10))

	require.True(t, scheduler.ShouldCheckpoint("ss", 10))
	require.True(t, scheduler.ShouldCheckpoint("receipt", 10))
}

// The height stays available after it completes: a store that reaches it later than the store that
// finished it is handed the same height rather than the next one.
func TestAPickedHeightStaysYesForALaggingStore(t *testing.T) {
	scheduler := newScheduler(0, 10)
	require.True(t, scheduler.ShouldCheckpoint("sc", 10))
	scheduler.MarkCheckpointComplete("sc", 10)

	require.True(t, scheduler.ShouldCheckpoint("sc", 10))
}

// A height answered no stays no once an interval elapses under it, so two stores asking either side
// of that moment cannot split on the same version.
func TestARejectedHeightStaysNoForEveryStore(t *testing.T) {
	scheduler := newScheduler(time.Hour, 0)
	scheduler.elapseTimeInterval()
	require.True(t, scheduler.ShouldCheckpoint("sc", 10))
	scheduler.MarkCheckpointComplete("sc", 10)

	require.False(t, scheduler.ShouldCheckpoint("sc", 11))

	scheduler.elapseTimeInterval()
	require.False(t, scheduler.ShouldCheckpoint("ss", 11), "11 was already answered no")
	require.True(t, scheduler.ShouldCheckpoint("sc", 12))
}

// The refusal covers every height at or under the one refused, not just that exact height. A store
// that asks just short of the interval pushes the floor up to the height it was at, so a store
// lagging behind it cannot walk in under that floor the moment the interval elapses and take a
// height the store ahead of it was already refused.
func TestALaggingStoreCannotTakeAHeightUnderARefusedOne(t *testing.T) {
	scheduler := newScheduler(5*time.Minute, 0)

	require.False(t, scheduler.ShouldCheckpoint("sc", 100), "the interval has not elapsed")

	scheduler.elapseTimeInterval()
	for _, lagging := range []int64{98, 99, 100} {
		require.False(t, scheduler.ShouldCheckpoint("ss", lagging), "height %d is under the refused 100", lagging)
	}

	require.True(t, scheduler.ShouldCheckpoint("sc", 101))
	require.True(t, scheduler.ShouldCheckpoint("ss", 101), "the lagging store reaches the same height")
}

func TestAHeightBelowTheCurrentOneIsNo(t *testing.T) {
	scheduler := newScheduler(0, 10)
	require.True(t, scheduler.ShouldCheckpoint("sc", 10))

	require.False(t, scheduler.ShouldCheckpoint("ss", 9))
}

func TestConcurrentAsksAtTheSameHeightAgree(t *testing.T) {
	scheduler := newScheduler(0, 10)

	var answers [8]bool
	var wg sync.WaitGroup
	for i := range answers {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			answers[i] = scheduler.ShouldCheckpoint("sc", 10)
		}(i)
	}
	wg.Wait()

	for i, answer := range answers {
		require.True(t, answer, "asker %d", i)
	}
}

// ---------------------------------------------------------------------------
// Stores at different heights
// ---------------------------------------------------------------------------

// walkLeaderAndLaggard runs two stores up to a height, the second one lag blocks behind the first,
// and returns the heights each checkpointed at. A non-zero elapseEvery elapses the time interval
// once every that many heights, standing in for wall-clock passing as the chain advances.
func walkLeaderAndLaggard(
	scheduler *CheckpointScheduler, to, lag, elapseEvery int64,
) (leaderTook, laggardTook []int64) {
	for height := int64(1); height <= to; height++ {
		if elapseEvery > 0 && height%elapseEvery == 0 {
			scheduler.elapseTimeInterval()
		}
		if scheduler.ShouldCheckpoint("leader", height) {
			leaderTook = append(leaderTook, height)
			scheduler.MarkCheckpointComplete("leader", height)
		}
		behind := height - lag
		if behind > 0 && scheduler.ShouldCheckpoint("laggard", behind) {
			laggardTook = append(laggardTook, behind)
			scheduler.MarkCheckpointComplete("laggard", behind)
		}
	}
	return leaderTook, laggardTook
}

// A store lagging by more than the block interval still takes every height the leader takes, since
// the height is held until it arrives. Waiting for it costs whole boundaries rather than shifting
// the grid: 300 passes while 200 is still held, so the next checkpoint is 400.
func TestEveryStoreTakesTheSameHeightsHoweverFarBehind(t *testing.T) {
	scheduler := newScheduler(0, 100)

	leaderTook, laggardTook := walkLeaderAndLaggard(scheduler, 1000, 150, 0)

	require.Equal(t, []int64{100, 200, 400, 600, 800, 1000}, leaderTook)
	require.Equal(t, []int64{200, 400, 600, 800}, laggardTook,
		"100 predates the laggard's first ask, and it has yet to reach 1000")
}

func TestEveryStoreTakesTheSameHeightsOnATimeInterval(t *testing.T) {
	scheduler := newScheduler(time.Hour, 0)

	leaderTook, laggardTook := walkLeaderAndLaggard(scheduler, 1000, 150, 300)

	require.Equal(t, []int64{300, 600, 900}, leaderTook)
	require.Equal(t, []int64{300, 600}, laggardTook, "the laggard has yet to reach 900")
}

// The height is held even once the intervals have elapsed, so the leader cannot run ahead onto a
// height the store behind it would never be offered.
func TestAHeightIsHeldUntilEveryStoreHasReportedIt(t *testing.T) {
	scheduler := newScheduler(time.Hour, 0)
	scheduler.elapseTimeInterval()
	require.True(t, scheduler.ShouldCheckpoint("sc", 100))
	require.True(t, scheduler.ShouldCheckpoint("ss", 100))
	scheduler.MarkCheckpointComplete("sc", 100)

	scheduler.elapseTimeInterval()
	require.False(t, scheduler.ShouldCheckpoint("sc", 200), "ss has not reported 100")

	scheduler.MarkCheckpointComplete("ss", 100)
	scheduler.elapseTimeInterval()
	require.True(t, scheduler.ShouldCheckpoint("sc", 300))
}

// A store whose first ask lands while a height is held is not one of the stores that height waits
// for: it may already be past that version, and holding for it would wedge the schedule.
func TestAStoreRegisteringWhileAHeightIsHeldJoinsTheNextOne(t *testing.T) {
	scheduler := newScheduler(time.Hour, 0)
	scheduler.elapseTimeInterval()
	require.True(t, scheduler.ShouldCheckpoint("sc", 100))

	require.False(t, scheduler.ShouldCheckpoint("ss", 150), "ss registers by asking")
	scheduler.MarkCheckpointComplete("sc", 100)

	scheduler.elapseTimeInterval()
	require.True(t, scheduler.ShouldCheckpoint("sc", 200))
	scheduler.MarkCheckpointComplete("sc", 200)
	scheduler.elapseTimeInterval()
	require.False(t, scheduler.ShouldCheckpoint("sc", 300), "200 is now held for ss as well")
}

// A store whose version jumps over the held height would otherwise hold it forever, since it never
// asks at that height again. Asking above it is proof it has passed, so it is released and only
// that store misses the checkpoint.
func TestAStoreThatPassesTheHeldHeightIsReleased(t *testing.T) {
	scheduler := newScheduler(time.Hour, 0)
	require.False(t, scheduler.ShouldCheckpoint("ss", 98), "ss registers before the interval elapses")

	scheduler.elapseTimeInterval()
	require.True(t, scheduler.ShouldCheckpoint("sc", 100))
	scheduler.MarkCheckpointComplete("sc", 100)

	require.False(t, scheduler.ShouldCheckpoint("ss", 101), "ss jumped from 99 to 101")

	scheduler.elapseTimeInterval()
	require.True(t, scheduler.ShouldCheckpoint("sc", 200), "100 is no longer held for ss")
}

// A store commits later versions while its own checkpoint of the held height is still running, so
// asking above that height is not proof it skipped it. Releasing it there would start the intervals
// while it is mid-write.
func TestAStoreWritingTheHeldHeightIsNotReleased(t *testing.T) {
	scheduler := newScheduler(time.Hour, 0)
	scheduler.elapseTimeInterval()
	require.True(t, scheduler.ShouldCheckpoint("sc", 100))

	require.False(t, scheduler.ShouldCheckpoint("sc", 101), "sc is still writing 100")

	scheduler.elapseTimeInterval()
	require.False(t, scheduler.ShouldCheckpoint("sc", 200), "100 is still held for sc")

	scheduler.MarkCheckpointComplete("sc", 100)
	scheduler.elapseTimeInterval()
	require.True(t, scheduler.ShouldCheckpoint("sc", 300))
}

// ---------------------------------------------------------------------------
// Completion
// ---------------------------------------------------------------------------

// A store that stops asking holds its height for good: no later height is picked however long the
// intervals have had to elapse, and nothing recovers short of a restart. A store still asking is
// released once it passes the height, so this is the stalled-store case rather than the skipped one.
func TestAStalledStoreStopsTheScheduler(t *testing.T) {
	scheduler := newScheduler(time.Hour, 0)
	scheduler.elapseTimeInterval()
	require.True(t, scheduler.ShouldCheckpoint("sc", 100))
	scheduler.MarkCheckpointComplete("sc", 100)

	// ss registers, is held for 200, then stops asking entirely.
	require.False(t, scheduler.ShouldCheckpoint("ss", 150))
	scheduler.elapseTimeInterval()
	require.True(t, scheduler.ShouldCheckpoint("sc", 200))
	scheduler.MarkCheckpointComplete("sc", 200)

	for _, height := range []int64{300, 400, 500} {
		scheduler.elapseTimeInterval()
		require.False(t, scheduler.ShouldCheckpoint("sc", height), "ss never reported 200")
	}
}

// Reporting a height whose checkpoint failed is what keeps the scheduler moving, which is why the
// call carries no success flag: a store defers it and the next height comes due as usual.
func TestReportingAFailedHeightKeepsTheSchedulerMoving(t *testing.T) {
	scheduler := newScheduler(time.Hour, 0)
	scheduler.elapseTimeInterval()
	require.True(t, scheduler.ShouldCheckpoint("sc", 100))

	scheduler.MarkCheckpointComplete("sc", 100)

	scheduler.elapseTimeInterval()
	require.True(t, scheduler.ShouldCheckpoint("sc", 200))
}

// The interval runs from the last store finishing rather than the first, so the gap to the next
// checkpoint is not spent by a store that is still writing this one.
func TestTheIntervalRunsFromTheLastStoreCompleting(t *testing.T) {
	scheduler := newScheduler(time.Hour, 0)
	scheduler.elapseTimeInterval()
	require.True(t, scheduler.ShouldCheckpoint("sc", 100))
	require.True(t, scheduler.ShouldCheckpoint("ss", 100))

	scheduler.MarkCheckpointComplete("sc", 100)
	afterFirst := scheduler.checkpointedAt
	scheduler.MarkCheckpointComplete("ss", 100)

	require.True(t, scheduler.checkpointedAt.After(afterFirst))
}

func TestMarkCheckpointCompleteIgnoresAnotherVersion(t *testing.T) {
	scheduler := newScheduler(time.Hour, 0)
	scheduler.elapseTimeInterval()
	require.True(t, scheduler.ShouldCheckpoint("sc", 100))

	scheduler.MarkCheckpointComplete("sc", 99)
	scheduler.MarkCheckpointComplete("sc", 101)

	require.False(t, scheduler.allStoresCheckpointed(), "neither version is the height being held")
}

func TestMarkCheckpointCompleteIgnoresAnUnregisteredStore(t *testing.T) {
	scheduler := newScheduler(time.Hour, 0)
	scheduler.elapseTimeInterval()
	require.True(t, scheduler.ShouldCheckpoint("sc", 100))
	require.True(t, scheduler.ShouldCheckpoint("ss", 100))

	scheduler.MarkCheckpointComplete("receipt", 100)

	require.False(t, scheduler.allStoresCheckpointed(), "the height is still held for sc and ss")
}

// A store reporting a height it already reported must not restart the intervals, which would push
// the next checkpoint out every time a straggler that took the height late reports.
func TestARepeatedReportDoesNotRestartTheIntervals(t *testing.T) {
	scheduler := newScheduler(time.Hour, 0)
	scheduler.elapseTimeInterval()
	require.True(t, scheduler.ShouldCheckpoint("sc", 100))

	scheduler.MarkCheckpointComplete("sc", 100)
	completedAt := scheduler.checkpointedAt
	scheduler.MarkCheckpointComplete("sc", 100)

	require.Equal(t, completedAt, scheduler.checkpointedAt)
}
