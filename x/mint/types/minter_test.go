package types

import (
	"testing"
	"time"

	"github.com/sei-protocol/sei-chain/x/epoch/types"
	"github.com/stretchr/testify/require"
)

func getGenesisTime() time.Time {
	return time.Date(2022, time.Month(7), 18, 10, 0, 0, 0, time.UTC)
}

func getEpoch(currTime time.Time) types.Epoch {
	genesisTime := getGenesisTime()
	// Epochs increase every minute, so derive based on the time
	return types.Epoch{
		GenesisTime:           genesisTime,
		EpochDuration:         time.Minute,
		CurrentEpoch:          uint64(currTime.Sub(genesisTime).Minutes()),
		CurrentEpochStartTime: currTime,
		CurrentEpochHeight:    0,
	}
}

func getTestTokenReleaseSchedule(currTime time.Time, numReleases int) []ScheduledTokenRelease {
	tokenReleaseSchedule := []ScheduledTokenRelease{}

	for i := 1; i <= numReleases; i++ {
		// Token release every year
		currTime = currTime.AddDate(1, 0, 0)
		scheduledRelease := ScheduledTokenRelease{Date: currTime.Format(TokenReleaseDateFormat), TokenReleaseAmount: 2500000 / int64(i)}
		tokenReleaseSchedule = append(tokenReleaseSchedule, scheduledRelease)
	}

	return tokenReleaseSchedule
}

// Next epoch provisions benchmarking
// cpu: Intel(R) Core(TM) i9-9880H CPU @ 2.30GHz
// BenchmarkGetScheduledTokenRelease-16               319.7 ns/op            56 B/op          3 allocs/op
func BenchmarkGetScheduledTokenRelease(b *testing.B) {
	b.ReportAllocs()

	genesisTime := getGenesisTime()
	epoch := getEpoch(genesisTime)
	tokenReleaseSchedule := getTestTokenReleaseSchedule(genesisTime, 10)

	// run the GetScheduledTokenRelease function b.N times
	for n := 0; n < b.N; n++ {
		GetScheduledTokenRelease(
			epoch,
			genesisTime,
			tokenReleaseSchedule,
		)
	}
}

func TestGetScheduledTokenReleaseNil(t *testing.T) {
	genesisTime := getGenesisTime()
	epoch := getEpoch(genesisTime.AddDate(20, 0, 0))
	tokenReleaseSchedule := getTestTokenReleaseSchedule(genesisTime, 10)

	scheduledTokenRelease := GetScheduledTokenRelease(
		epoch,
		genesisTime.AddDate(10, 0, 0),
		tokenReleaseSchedule,
	)
	// Should return nil if there are no scheduled releases
	require.Nil(t, scheduledTokenRelease)
}

func TestGetScheduledTokenRelease(t *testing.T) {
	genesisTime := getGenesisTime()
	epoch := getEpoch(genesisTime.AddDate(5, 0, 0))
	tokenReleaseSchedule := getTestTokenReleaseSchedule(genesisTime, 10)

	scheduledTokenRelease := GetScheduledTokenRelease(
		epoch,
		genesisTime.AddDate(4, 0, 0),
		tokenReleaseSchedule,
	)

	require.NotNil(t, scheduledTokenRelease)
	require.Equal(t, scheduledTokenRelease.GetTokenReleaseAmount(), int64(2500000/5))
	require.Equal(t, scheduledTokenRelease.GetDate(), genesisTime.AddDate(5, 0, 0).Format(TokenReleaseDateFormat))
}

func TestGetScheduledTokenReleaseOverdue(t *testing.T) {
	genesisTime := getGenesisTime()
	tokenReleaseSchedule := getTestTokenReleaseSchedule(genesisTime, 10)
	scheduledTokenRelease := GetScheduledTokenRelease(
		// 10 days past the first token release schedule
		// possible if the chain was down more than a day
		getEpoch(genesisTime.AddDate(3, 0, 10)),
		// Last release was the year before
		genesisTime.AddDate(2, 0, 0),
		tokenReleaseSchedule,
	)

	require.NotNil(t, scheduledTokenRelease)
	// Year 3 release should still happen (second time)
	require.Equal(t, scheduledTokenRelease.GetTokenReleaseAmount(), int64(2500000 / 3))
	require.Equal(t, scheduledTokenRelease.GetDate(), genesisTime.AddDate(3, 0, 0).Format(TokenReleaseDateFormat))
}

func TestParamsUsei(t *testing.T) {
	params := DefaultParams()
	err := params.Validate()
	require.Nil(t, err)

	params.MintDenom = "sei"
	err = params.Validate()
	require.NotNil(t, err)
}
