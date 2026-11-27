package distribution

import (
	"math/big"
	"testing"

	"github.com/stretchr/testify/require"
)

const day = 24 * 3600 * second

// minting writes schedule at now and returns the store.
func minting(t *testing.T, now uint64, schedule ...ReleasePeriod) store {
	state := newMemState()
	require.NoError(t, WriteReleaseSchedule(state, schedule, now))
	return newStore(state)
}

// requireMints requires minting up to t to release want.
func requireMints(t *testing.T, s store, at uint64, want int64, msg string) {
	t.Helper()
	got, err := s.mint(at)
	require.NoError(t, err)
	require.Equal(t, big.NewInt(want), got, msg)
}

func period(start, end uint64, amount int64) ReleasePeriod {
	return ReleasePeriod{StartTime: start, EndTime: end, Amount: big.NewInt(amount)}
}

func TestMintReleasesAPeriodEvenlyOverItsTime(t *testing.T) {
	s := minting(t, 0, period(0, 10*day, 1000))
	requireMints(t, s, day, 100, "a tenth of the period")
	requireMints(t, s, day+day/2, 50, "half a day")
	requireMints(t, s, day+day/2, 0, "no time has passed")
	requireMints(t, s, day, 0, "time before the last mint releases nothing")
	requireMints(t, s, 10*day, 850, "the period's end releases what is left")
	requireMints(t, s, 11*day, 0, "the schedule is over")
}

func TestMintCarriesTruncationToTheNextMint(t *testing.T) {
	s := minting(t, 0, period(0, 3, 10))
	requireMints(t, s, 1, 3, "10/3 truncated")
	requireMints(t, s, 2, 3, "7/2 truncated")
	requireMints(t, s, 3, 4, "the rest")
}

func TestMintSkipsGapsAndCatchesUpOverPeriods(t *testing.T) {
	s := minting(t, 0, period(0, 10, 100), period(20, 30, 100), period(30, 40, 100))
	requireMints(t, s, 15, 100, "the first period ended at 10")
	requireMints(t, s, 18, 0, "a gap releases nothing")
	requireMints(t, s, 25, 50, "from the second period's start")
	requireMints(t, s, 35, 100, "the rest of the second, and half the third")
	requireMints(t, s, 100, 50, "the rest of the third")
	m := s.mintState()
	require.Equal(t, uint64(0), m.periodEnd, "no period is copied in")
	require.Equal(t, uint64(40), m.lastMintTime)
	require.Zero(t, m.remaining.Sign())
}

func TestMintStartsWhereGenesisDoes(t *testing.T) {
	s := minting(t, 5, period(0, 10, 100))
	requireMints(t, s, 10, 100, "a period in progress releases its whole amount over what is left")

	s = minting(t, 12, period(0, 10, 100), period(10, 20, 100))
	requireMints(t, s, 20, 100, "a period ended by genesis releases nothing")
}

func TestMintWithoutAScheduleReleasesNothing(t *testing.T) {
	requireMints(t, newStore(newMemState()), day, 0, "")
	requireMints(t, minting(t, 0), day, 0, "an empty schedule")
}

func TestWriteReleaseScheduleChecksTheSchedule(t *testing.T) {
	for name, schedule := range map[string][]ReleasePeriod{
		"empty period":       {period(5, 5, 1)},
		"overlap":            {period(0, 10, 1), period(9, 20, 1)},
		"out of order":       {period(10, 20, 1), period(0, 5, 1)},
		"negative amount":    {period(0, 10, -1)},
		"no amount":          {{StartTime: 0, EndTime: 10}},
		"amount over 256bit": {{StartTime: 0, EndTime: 10, Amount: new(big.Int).Lsh(big.NewInt(1), 256)}},
		"too many periods":   make([]ReleasePeriod, maxReleasePeriods+1),
	} {
		require.ErrorIs(t, WriteReleaseSchedule(newMemState(), schedule, 0), errInvalidSchedule, name)
	}
	state := newMemState()
	require.NoError(t, WriteReleaseSchedule(state, []ReleasePeriod{period(0, 10, 1), period(10, 20, 0)}, 0))
	require.ErrorIs(t, WriteReleaseSchedule(state, []ReleasePeriod{period(0, 10, 1)}, 0), errScheduleWritten)
}
