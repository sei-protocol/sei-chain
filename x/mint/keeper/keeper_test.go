package keeper_test

import (
	"testing"
	"time"

	epochTypes "github.com/sei-protocol/sei-chain/x/epoch/types"
	mintKeeper "github.com/sei-protocol/sei-chain/x/mint/keeper"
	"github.com/sei-protocol/sei-chain/x/mint/types"
	mintTypes "github.com/sei-protocol/sei-chain/x/mint/types"
	minttypes "github.com/sei-protocol/sei-chain/x/mint/types"
	"github.com/stretchr/testify/require"
)

func TestGetNextScheduledTokenRelease(t *testing.T) {
	t.Parallel()

	currentTime := time.Now().UTC()
	epoch := epochTypes.Epoch{
		CurrentEpochStartTime: currentTime,
		CurrentEpochHeight: 100,
	}
	currentMinter := mintTypes.DefaultInitialMinter()

	tokenReleaseSchedule := []mintTypes.ScheduledTokenRelease{
		{
			StartDate: currentTime.AddDate(0, 0, 30).Format(minttypes.TokenReleaseDateFormat),
			EndDate: currentTime.AddDate(0, 2, 0).Format(minttypes.TokenReleaseDateFormat),
			TokenReleaseAmount: 200,
		},
		{
			StartDate: currentTime.AddDate(1, 0, 0).Format(minttypes.TokenReleaseDateFormat),
			EndDate: currentTime.AddDate(2, 0, 0).Format(minttypes.TokenReleaseDateFormat),
			TokenReleaseAmount: 300,
		},
		{
			StartDate: currentTime.AddDate(0, 0, 1).Format(minttypes.TokenReleaseDateFormat),
			EndDate: currentTime.AddDate(0, 0, 15).Format(minttypes.TokenReleaseDateFormat),
			TokenReleaseAmount: 100,
		},
	}

	t.Run("Get the next scheduled token release", func(t *testing.T) {
		// No next scheduled token release intially
		epoch.CurrentEpochStartTime = currentTime.AddDate(0, 0, 0)
		nextScheduledRelease := mintKeeper.GetNextScheduledTokenRelease(epoch, tokenReleaseSchedule, currentMinter)
		require.Nil(t, nextScheduledRelease)

		epoch.CurrentEpochStartTime = currentTime.AddDate(0, 0, 1)
		nextScheduledRelease = mintKeeper.GetNextScheduledTokenRelease(epoch, tokenReleaseSchedule, currentMinter)
		require.NotNil(t, nextScheduledRelease)
		require.Equal(t, uint64(100), nextScheduledRelease.TokenReleaseAmount)
	})

	t.Run("No next scheduled token release, assume we are on the second period", func(t *testing.T) {
		// No next scheduled token release intially
		epoch.CurrentEpochStartTime = currentTime.AddDate(0, 0, 0)
		nextScheduledRelease := mintKeeper.GetNextScheduledTokenRelease(epoch, tokenReleaseSchedule, currentMinter)
		require.Nil(t, nextScheduledRelease)

		secondMinter := mintTypes.NewMinter(
			currentTime.AddDate(0, 0, 30).Format(minttypes.TokenReleaseDateFormat),
			currentTime.AddDate(0, 2, 0).Format(minttypes.TokenReleaseDateFormat),
			"usei",
			200,
		)
		epoch.CurrentEpochStartTime = currentTime.AddDate(0, 5, 0)
		nextScheduledRelease = mintKeeper.GetNextScheduledTokenRelease(epoch, tokenReleaseSchedule, secondMinter)
		require.Nil(t, nextScheduledRelease)
	})

	t.Run("test case where we skip the start date due to outage for two days", func(t *testing.T) {
		// No next scheduled token release intially
		epoch.CurrentEpochStartTime = currentTime.AddDate(0, 0, 0)
		nextScheduledRelease := mintKeeper.GetNextScheduledTokenRelease(epoch, tokenReleaseSchedule, currentMinter)
		require.Nil(t, nextScheduledRelease)

		// First mint was +1 but the chain recoverd on +3
		epoch.CurrentEpochStartTime = currentTime.AddDate(0, 0, 3)
		nextScheduledRelease = mintKeeper.GetNextScheduledTokenRelease(epoch, tokenReleaseSchedule, currentMinter)
		require.Equal(t, uint64(100), nextScheduledRelease.GetTokenReleaseAmount())
		require.Equal(t, currentTime.AddDate(0, 0, 1).Format(minttypes.TokenReleaseDateFormat), nextScheduledRelease.GetStartDate())
	})
}

func TestGetOrUpdateLatestMinter(t *testing.T) {
	t.Parallel()
	app, ctx := createTestApp(false)
	mintKeeper := app.MintKeeper
	currentTime := time.Now()
	epoch := epochTypes.Epoch{
		CurrentEpochStartTime: currentTime,
	}

	t.Run("No ongoing release", func(t *testing.T) {
		currentMinter := mintKeeper.GetOrUpdateLatestMinter(ctx, epoch)
		require.False(t, currentMinter.OngoingRelease())
	})

	t.Run("No ongoing release, but there's a scheduled release", func(t *testing.T) {
		mintKeeper.SetMinter(ctx, mintTypes.NewMinter(
			currentTime.Format(minttypes.TokenReleaseDateFormat),
			currentTime.AddDate(1,0,0).Format(minttypes.TokenReleaseDateFormat),
			"usei",
			1000,
		))
		epoch.CurrentEpochStartTime = currentTime
		currentMinter := mintKeeper.GetOrUpdateLatestMinter(ctx, epoch)
		require.True(t, currentMinter.OngoingRelease())
		require.Equal(t, currentTime.Format(minttypes.TokenReleaseDateFormat), currentMinter.StartDate)
		mintKeeper.SetMinter(ctx, mintTypes.DefaultInitialMinter())
	})

	t.Run("Ongoing release same day", func(t *testing.T) {
		params := mintKeeper.GetParams(ctx)
		params.TokenReleaseSchedule = []types.ScheduledTokenRelease{
			{
				StartDate:          currentTime.AddDate(0,0,0).Format(minttypes.TokenReleaseDateFormat),
				EndDate:            currentTime.AddDate(0,0,0).Format(minttypes.TokenReleaseDateFormat),
				TokenReleaseAmount: 1000,
			},
		}
		mintKeeper.SetParams(ctx, params)

		minter := types.Minter{
			StartDate: currentTime.Format(minttypes.TokenReleaseDateFormat),
			EndDate: currentTime.Format(minttypes.TokenReleaseDateFormat),
			Denom: "usei",
			TotalMintAmount: 100,
			RemainingMintAmount: 0,
			LastMintAmount: 100,
			LastMintDate: "2023-04-01",
			LastMintHeight: 0,
		}
		mintKeeper.SetMinter(ctx, minter)

		epoch.CurrentEpochStartTime = currentTime
		currentMinter := mintKeeper.GetOrUpdateLatestMinter(ctx, epoch)
		amount := currentMinter.GetReleaseAmountToday(currentTime).IsZero()
		require.Zero(t, currentMinter.GetRemainingMintAmount())
		require.True(t, amount)
		require.False(t, currentMinter.OngoingRelease())
		require.Equal(t, currentTime.Format(minttypes.TokenReleaseDateFormat), currentMinter.StartDate)
		mintKeeper.SetMinter(ctx, mintTypes.DefaultInitialMinter())
	})


	t.Run("TokenReleaseSchedule not sorted", func(t *testing.T) {
		params := mintKeeper.GetParams(ctx)
		params.TokenReleaseSchedule = []types.ScheduledTokenRelease{
			{
				StartDate:          currentTime.AddDate(0,20,0).Format(minttypes.TokenReleaseDateFormat),
				EndDate:            currentTime.AddDate(0,45,0).Format(minttypes.TokenReleaseDateFormat),
				TokenReleaseAmount: 2000,
			},
			{
				StartDate:          currentTime.Format(minttypes.TokenReleaseDateFormat),
				EndDate:            currentTime.AddDate(0,15,0).Format(minttypes.TokenReleaseDateFormat),
				TokenReleaseAmount: 1000,
			},
		}
		mintKeeper.SetParams(ctx, params)

		epoch.CurrentEpochStartTime = currentTime
		currentMinter := mintKeeper.GetOrUpdateLatestMinter(ctx, epoch)
		require.True(t, currentMinter.OngoingRelease())
		require.Equal(t, currentTime.Format(minttypes.TokenReleaseDateFormat), currentMinter.StartDate)
	})
}
