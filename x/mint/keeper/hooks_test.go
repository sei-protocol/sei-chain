package keeper_test

import (
	"testing"
	"time"

	keepertest "github.com/sei-protocol/sei-chain/testutil/keeper"
	"github.com/sei-protocol/sei-chain/x/epoch/types"
	minttypes "github.com/sei-protocol/sei-chain/x/mint/types"
	"github.com/stretchr/testify/require"
	abci "github.com/tendermint/tendermint/abci/types"

	tmproto "github.com/tendermint/tendermint/proto/tendermint/types"
)

func getGenesisTime() time.Time {
	return time.Date(2022, time.Month(7), 18, 10, 0, 0, 0, time.UTC)
}

func getEpoch(genesisTime time.Time, currTime time.Time) types.Epoch {
	// Epochs increase every minute, so derive based on the time
	return types.Epoch{
		GenesisTime:           genesisTime,
		EpochDuration:         time.Minute,
		CurrentEpoch:          uint64(currTime.Sub(genesisTime).Minutes()),
		CurrentEpochStartTime: currTime,
		CurrentEpochHeight:    0,
	}
}

func TestEndOfEpochMintedCoinDistribution(t *testing.T) {
	t.Parallel()

	t.Run("Initial should be zero", func(t *testing.T) {
		seiApp := keepertest.TestApp()
		ctx := seiApp.BaseApp.NewContext(false, tmproto.Header{Time: time.Now()})

		header := tmproto.Header{
			Height: seiApp.LastBlockHeight() + 1,
			Time:   time.Now().UTC(),
		}
		seiApp.BeginBlock(ctx, abci.RequestBeginBlock{Header: header})
		require.Equal(t, int64(0), seiApp.MintKeeper.GetMinter(ctx).GetLastMintAmountCoin().Amount.Int64())
	})

	t.Run("even full release", func(t *testing.T) {
		seiApp := keepertest.TestApp()
		ctx := seiApp.BaseApp.NewContext(false, tmproto.Header{Time: time.Now()})

		header := tmproto.Header{
			Height: seiApp.LastBlockHeight() + 1,
			Time:   time.Now().UTC(),
		}
		seiApp.BeginBlock(ctx, abci.RequestBeginBlock{Header: header})
		genesisTime := header.Time
		tokenReleaseSchedle := []minttypes.ScheduledTokenRelease{
			{
				StartDate:          genesisTime.AddDate(0, 0, 0).Format(minttypes.TokenReleaseDateFormat),
				EndDate:            genesisTime.AddDate(0, 0, 25).Format(minttypes.TokenReleaseDateFormat),
				TokenReleaseAmount: 2500000,
			},
		}
		mintParams := minttypes.NewParams(
			"usei",
			tokenReleaseSchedle,
		)
		seiApp.MintKeeper.SetParams(ctx, mintParams)

		for i := 0; i < 25; i++ {
			currTime := genesisTime.AddDate(0, 0, i)
			currEpoch := getEpoch(genesisTime, currTime)
			seiApp.EpochKeeper.BeforeEpochStart(ctx, currEpoch)
			seiApp.EpochKeeper.AfterEpochEnd(ctx, currEpoch)
			mintParams = seiApp.MintKeeper.GetParams(ctx)

			// 250k / 25 days = 100000 per day
			expectedAmount := int64(100000)
			newMinter := seiApp.MintKeeper.GetMinter(ctx)

			if i == 24 {
				require.Zero(t, newMinter.GetRemainingMintAmount(), "Remaining amount should be zero")
				break
			}

			require.Equal(t, currTime.Format(minttypes.TokenReleaseDateFormat), newMinter.GetLastMintDate(), "Last mint date should be correct")
			require.Equal(t, expectedAmount, newMinter.GetLastMintAmountCoin().Amount.Int64(), "Minted amount should be correct")
			require.Equal(t, int64(2500000-expectedAmount*int64(i+1)), int64(newMinter.GetRemainingMintAmount()), "Remaining amount should be correct")
		}
	})

	t.Run("uneven full release", func(t *testing.T) {
		seiApp := keepertest.TestApp()
		ctx := seiApp.BaseApp.NewContext(false, tmproto.Header{Time: time.Now()})

		header := tmproto.Header{
			Height: seiApp.LastBlockHeight() + 1,
			Time:   time.Now().UTC(),
		}
		seiApp.BeginBlock(ctx, abci.RequestBeginBlock{Header: header})
		genesisTime := header.Time

		tokenReleaseSchedle := []minttypes.ScheduledTokenRelease{
			{
				StartDate:          genesisTime.AddDate(0, 0, 0).Format(minttypes.TokenReleaseDateFormat),
				EndDate:            genesisTime.AddDate(0, 0, 24).Format(minttypes.TokenReleaseDateFormat),
				TokenReleaseAmount: 2500000,
			},
		}
		mintParams := minttypes.NewParams(
			"usei",
			tokenReleaseSchedle,
		)
		seiApp.MintKeeper.SetParams(ctx, mintParams)

		for i := 0; i < 25; i++ {
			currTime := genesisTime.AddDate(0, 0, i)
			currEpoch := getEpoch(genesisTime, currTime)
			seiApp.EpochKeeper.BeforeEpochStart(ctx, currEpoch)
			seiApp.EpochKeeper.AfterEpochEnd(ctx, currEpoch)
			mintParams = seiApp.MintKeeper.GetParams(ctx)

			expectedAmount := int64(104166)
			newMinter := seiApp.MintKeeper.GetMinter(ctx)

			// Uneven distribution still results in 250k total distributed
			if i == 24 {
				require.Zero(t, newMinter.GetRemainingMintAmount(), "Remaining amount should be zero")
				break
			}

			require.Equal(t, currTime.Format(minttypes.TokenReleaseDateFormat), newMinter.GetLastMintDate(), "Last mint date should be correct")
			require.InDelta(t, expectedAmount, newMinter.GetLastMintAmountCoin().Amount.Int64(), 1, "Minted amount should be correct")
			require.InDelta(t, int64(2500000-expectedAmount*int64(i+1)), int64(newMinter.GetRemainingMintAmount()), 24, "Remaining amount should be correct")
		}
	})

	t.Run("multiple full releases", func(t *testing.T) {
		seiApp := keepertest.TestApp()
		ctx := seiApp.BaseApp.NewContext(false, tmproto.Header{Time: time.Now()})

		header := tmproto.Header{
			Height: seiApp.LastBlockHeight() + 1,
			Time:   time.Now().UTC(),
		}
		seiApp.BeginBlock(ctx, abci.RequestBeginBlock{Header: header})
		genesisTime := header.Time

		tokenReleaseSchedle := []minttypes.ScheduledTokenRelease{
			{
				StartDate:          genesisTime.AddDate(0, 0, 0).Format(minttypes.TokenReleaseDateFormat),
				EndDate:            genesisTime.AddDate(0, 0, 24).Format(minttypes.TokenReleaseDateFormat),
				TokenReleaseAmount: 2500000,
			},
			{
				StartDate:          genesisTime.AddDate(0, 0, 24).Format(minttypes.TokenReleaseDateFormat),
				EndDate:            genesisTime.AddDate(0, 0, 30).Format(minttypes.TokenReleaseDateFormat),
				TokenReleaseAmount: 2500000,
			},
			{
				StartDate:          genesisTime.AddDate(0, 0, 30).Format(minttypes.TokenReleaseDateFormat),
				EndDate:            genesisTime.AddDate(0, 0, 40).Format(minttypes.TokenReleaseDateFormat),
				TokenReleaseAmount: 2500000,
			},
			{
				StartDate:          genesisTime.AddDate(0, 0, 45).Format(minttypes.TokenReleaseDateFormat),
				EndDate:            genesisTime.AddDate(0, 0, 50).Format(minttypes.TokenReleaseDateFormat),
				TokenReleaseAmount: 2500000,
			},
		}
		mintParams := minttypes.NewParams(
			"usei",
			tokenReleaseSchedle,
		)
		seiApp.MintKeeper.SetParams(ctx, mintParams)

		for i := 0; i < 50; i++ {
			currTime := genesisTime.AddDate(0, 0, i)
			currEpoch := getEpoch(genesisTime, currTime)
			seiApp.EpochKeeper.BeforeEpochStart(ctx, currEpoch)
			seiApp.EpochKeeper.AfterEpochEnd(ctx, currEpoch)
			mintParams = seiApp.MintKeeper.GetParams(ctx)

			newMinter := seiApp.MintKeeper.GetMinter(ctx)

			// Should be zero by the end of each release and when there's no release scheduled
			if i == 23 || i == 29 || i == 39 || i == 49 || (i >= 40 && i < 45) {
				require.Zero(t, newMinter.GetRemainingMintAmount(), "Remaining amount should be zero at %s", currTime.Format(minttypes.TokenReleaseDateFormat))
				continue
			}

			require.Equal(t, currTime.Format(minttypes.TokenReleaseDateFormat), newMinter.GetLastMintDate(), "Last mint date should be correct")
		}
	})

	t.Run("outage during release", func(t *testing.T) {
		seiApp := keepertest.TestApp()
		ctx := seiApp.BaseApp.NewContext(false, tmproto.Header{Time: time.Now()})

		header := tmproto.Header{
			Height: seiApp.LastBlockHeight() + 1,
			Time:   time.Now().UTC(),
		}
		seiApp.BeginBlock(ctx, abci.RequestBeginBlock{Header: header})
		genesisTime := header.Time

		tokenReleaseSchedle := []minttypes.ScheduledTokenRelease{
			{
				StartDate:          genesisTime.AddDate(0, 0, 0).Format(minttypes.TokenReleaseDateFormat),
				EndDate:            genesisTime.AddDate(0, 0, 24).Format(minttypes.TokenReleaseDateFormat),
				TokenReleaseAmount: 2500000,
			},
		}
		mintParams := minttypes.NewParams(
			"usei",
			tokenReleaseSchedle,
		)
		seiApp.MintKeeper.SetParams(ctx, mintParams)

		for i := 0; i < 13; i++ {
			currTime := genesisTime.AddDate(0, 0, i)
			currEpoch := getEpoch(genesisTime, currTime)
			seiApp.EpochKeeper.BeforeEpochStart(ctx, currEpoch)
			seiApp.EpochKeeper.AfterEpochEnd(ctx, currEpoch)
			mintParams = seiApp.MintKeeper.GetParams(ctx)

			newMinter := seiApp.MintKeeper.GetMinter(ctx)
			expectedAmount := int64(104166)

			require.Equal(t, currTime.Format(minttypes.TokenReleaseDateFormat), newMinter.GetLastMintDate(), "Last mint date should be correct")
			require.InDelta(t, expectedAmount, newMinter.GetLastMintAmountCoin().Amount.Int64(), 1, "Minted amount should be correct")
			require.InDelta(t, int64(2500000-expectedAmount*int64(i+1)), int64(newMinter.GetRemainingMintAmount()), 24, "Remaining amount should be correct")
		}

		// 3 day outage
		postOutageTime := genesisTime.AddDate(0, 0, 15)
		currEpoch := getEpoch(genesisTime, postOutageTime)
		seiApp.EpochKeeper.BeforeEpochStart(ctx, currEpoch)
		seiApp.EpochKeeper.AfterEpochEnd(ctx, currEpoch)
		mintParams = seiApp.MintKeeper.GetParams(ctx)

		newMinter := seiApp.MintKeeper.GetMinter(ctx)
		require.Equal(t, postOutageTime.Format(minttypes.TokenReleaseDateFormat), newMinter.GetLastMintDate(), "Last mint date should be correct")
		require.InDelta(t, 127315, newMinter.GetLastMintAmountCoin().Amount.Int64(), 1, "Minted amount should be correct")
		require.InDelta(t, int64(1018522), int64(newMinter.GetRemainingMintAmount()), 24, "Remaining amount should be correct")

		// Continue and ensure that eventually reaches zero
		for i := 16; i < 25; i++ {
			currTime := genesisTime.AddDate(0, 0, i)
			currEpoch := getEpoch(genesisTime, currTime)
			seiApp.EpochKeeper.BeforeEpochStart(ctx, currEpoch)
			seiApp.EpochKeeper.AfterEpochEnd(ctx, currEpoch)
			mintParams = seiApp.MintKeeper.GetParams(ctx)

			newMinter := seiApp.MintKeeper.GetMinter(ctx)
			expectedAmount := int64(127315)

			if i == 24 {
				require.Zero(t, newMinter.GetRemainingMintAmount(), "Remaining amount should be zero")
				break
			}

			require.Equal(t, currTime.Format(minttypes.TokenReleaseDateFormat), newMinter.GetLastMintDate(), "Last mint date should be correct")
			require.InDelta(t, expectedAmount, newMinter.GetLastMintAmountCoin().Amount.Int64(), 1, "Minted amount should be correct")
		}

	})
}

func TestNoEpochPassedNoDistribution(t *testing.T) {
	seiApp := keepertest.TestApp()
	ctx := seiApp.BaseApp.NewContext(false, tmproto.Header{Time: time.Now()})

	header := tmproto.Header{Height: seiApp.LastBlockHeight() + 1}
	seiApp.BeginBlock(ctx, abci.RequestBeginBlock{Header: header})
	// Get mint params
	mintParams := seiApp.MintKeeper.GetParams(ctx)
	genesisTime := time.Date(2022, time.Month(7), 18, 10, 0, 0, 0, time.UTC)
	presupply := seiApp.BankKeeper.GetSupply(ctx, mintParams.MintDenom)
	startLastMintAmount := seiApp.MintKeeper.GetMinter(ctx).GetLastMintAmountCoin()
	// Loops through epochs under a year
	for i := 0; i < 60*24*7*52-1; i++ {
		currTime := genesisTime.Add(time.Minute)
		currEpoch := getEpoch(genesisTime, currTime)
		// Run hooks
		seiApp.EpochKeeper.BeforeEpochStart(ctx, currEpoch)
		seiApp.EpochKeeper.AfterEpochEnd(ctx, currEpoch)
		// Verify supply is the same and no coins have been minted
		currSupply := seiApp.BankKeeper.GetSupply(ctx, mintParams.MintDenom)
		require.True(t, currSupply.IsEqual(presupply))
	}
	// Ensure that EpochProvision hasn't changed
	endLastMintAmount := seiApp.MintKeeper.GetMinter(ctx).GetLastMintAmountCoin()
	require.True(t, startLastMintAmount.Equal(endLastMintAmount))
}

func TestSortTokenReleaseCalendar(t *testing.T) {
	testCases := []struct {
		name           string
		input          []minttypes.ScheduledTokenRelease
		expectedOutput []minttypes.ScheduledTokenRelease
	}{
		{
			name:           "Empty schedule",
			input:          []minttypes.ScheduledTokenRelease{},
			expectedOutput: []minttypes.ScheduledTokenRelease{},
		},
		{
			name: "Already sorted schedule",
			input: []minttypes.ScheduledTokenRelease{
				{StartDate: "2023-04-01", EndDate: "2023-04-10", TokenReleaseAmount: 100},
				{StartDate: "2023-04-11", EndDate: "2023-04-20", TokenReleaseAmount: 200},
				{StartDate: "2023-04-21", EndDate: "2023-04-30", TokenReleaseAmount: 300},
			},
			expectedOutput: []minttypes.ScheduledTokenRelease{
				{StartDate: "2023-04-01", EndDate: "2023-04-10", TokenReleaseAmount: 100},
				{StartDate: "2023-04-11", EndDate: "2023-04-20", TokenReleaseAmount: 200},
				{StartDate: "2023-04-21", EndDate: "2023-04-30", TokenReleaseAmount: 300},
			},
		},
		{
			name: "Unsorted schedule",
			input: []minttypes.ScheduledTokenRelease{
				{StartDate: "2023-04-21", EndDate: "2023-04-30", TokenReleaseAmount: 300},
				{StartDate: "2023-04-01", EndDate: "2023-04-10", TokenReleaseAmount: 100},
				{StartDate: "2023-04-11", EndDate: "2023-04-20", TokenReleaseAmount: 200},
			},
			expectedOutput: []minttypes.ScheduledTokenRelease{
				{StartDate: "2023-04-01", EndDate: "2023-04-10", TokenReleaseAmount: 100},
				{StartDate: "2023-04-11", EndDate: "2023-04-20", TokenReleaseAmount: 200},
				{StartDate: "2023-04-21", EndDate: "2023-04-30", TokenReleaseAmount: 300},
			},
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			sorted := minttypes.SortTokenReleaseCalendar(tc.input)

			if len(sorted) != len(tc.expectedOutput) {
				t.Fatalf("Expected output length to be %d, but got %d", len(tc.expectedOutput), len(sorted))
			}

			for i, expected := range tc.expectedOutput {
				if sorted[i].StartDate != expected.StartDate ||
					sorted[i].EndDate != expected.EndDate ||
					sorted[i].TokenReleaseAmount != expected.TokenReleaseAmount {
					t.Errorf("Expected token release at index %d to be %+v, but got %+v", i, expected, sorted[i])
				}
			}
		})
	}
}
