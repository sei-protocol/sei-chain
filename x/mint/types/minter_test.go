package types

import (
	"math/rand"
	"testing"
	"time"

	sdk "github.com/cosmos/cosmos-sdk/types"
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
		scheduledRelease := ScheduledTokenRelease{Date: currTime.AddDate(1, 0, 0).Format(TokenReleaseDateFormat), TokenReleaseAmount: 2500000 / int64(i)}
		tokenReleaseSchedule = append(tokenReleaseSchedule, scheduledRelease)
	}

	return tokenReleaseSchedule
}

// Benchmarking :)
// previously using sdk.Int operations:
// BenchmarkEpochProvision-4 5000000 220 ns/op
//
// using sdk.Dec operations: (current implementation)
// BenchmarkEpochProvision-4 3000000 429 ns/op
func BenchmarkEpochProvision(b *testing.B) {
	b.ReportAllocs()
	minter := InitialMinter()
	params := DefaultParams()

	s1 := rand.NewSource(100)
	r1 := rand.New(s1)
	minter.EpochProvisions = sdk.NewDec(r1.Int63n(1000000))

	// run the EpochProvision function b.N times
	for n := 0; n < b.N; n++ {
		minter.EpochProvision(params)
	}
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
		genesisTime,
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
		genesisTime,
		tokenReleaseSchedule,
	)

	require.NotNil(t, scheduledTokenRelease)
	require.Equal(t, scheduledTokenRelease.GetTokenReleaseAmount(), int64(2500000/4))
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
	require.Equal(t, scheduledTokenRelease.GetTokenReleaseAmount(), int64(2500000 / 2))
	require.Equal(t, scheduledTokenRelease.GetDate(), genesisTime.AddDate(3, 0, 0).Format(TokenReleaseDateFormat))
}
