package types_test

import (
	"testing"
	"time"

	tmproto "github.com/tendermint/tendermint/proto/tendermint/types"

	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/sei-protocol/sei-chain/app"
	epochTypes "github.com/sei-protocol/sei-chain/x/epoch/types"
	"github.com/sei-protocol/sei-chain/x/mint/types"
	"github.com/stretchr/testify/require"
)

func TestParamsUsei(t *testing.T) {
	params := types.DefaultParams()
	err := params.Validate()
	require.Nil(t, err)

	params.MintDenom = "sei"
	err = params.Validate()
	require.NotNil(t, err)
}

func TestDaysBetween(t *testing.T) {
	t.Parallel()
	testCases := []struct {
		name     string
		date1    string
		date2    string
		expected uint64
	}{
		{
			name:     "Same day",
			date1:    "2023-04-20T00:00:00Z",
			date2:    "2023-04-20T23:59:59Z",
			expected: 0,
		},
		{
			name:     "25 days apart",
			date1:    "2023-04-24T00:00:00Z",
			date2:    "2023-05-19T00:00:00Z",
			expected: 25,
		},
		{
			name:     "One day apart",
			date1:    "2023-04-20T00:00:00Z",
			date2:    "2023-04-21T00:00:00Z",
			expected: 1,
		},
		{
			name:     "Five days apart",
			date1:    "2023-04-20T00:00:00Z",
			date2:    "2023-04-25T00:00:00Z",
			expected: 5,
		},
		{
			name:     "Inverted dates",
			date1:    "2023-04-25T00:00:00Z",
			date2:    "2023-04-20T00:00:00Z",
			expected: 5,
		},
		{
			name:     "Less than 24 hours apart, crossing day boundary",
			date1:    "2023-04-20T23:00:00Z",
			date2:    "2023-04-21T22:59:59Z",
			expected: 1,
		},
		{
			name:     "Exactly 24 hours apart",
			date1:    "2023-04-20T12:34:56Z",
			date2:    "2023-04-21T12:34:56Z",
			expected: 1,
		},
		{
			name:     "One minute less than 24 hours apart",
			date1:    "2023-04-20T12:34:56Z",
			date2:    "2023-04-21T12:33:56Z",
			expected: 1,
		},
		{
			name:     "Inverted dates with times",
			date1:    "2023-04-25T15:30:00Z",
			date2:    "2023-04-20T10:00:00Z",
			expected: 5,
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			date1, _ := time.Parse(time.RFC3339, tc.date1)
			date2, _ := time.Parse(time.RFC3339, tc.date2)

			result := types.DaysBetween(date1, date2)

			if result != tc.expected {
				t.Errorf("Expected days between %s and %s to be %d, but got %d", tc.date1, tc.date2, tc.expected, result)
			}
		})
	}
}

func TestGetReleaseAmountToday(t *testing.T) {
	t.Parallel()
	testCases := []struct {
		name           string
		minter         types.Minter
		currentTime    time.Time
		expectedAmount uint64
	}{
		{
			name: "Regular scenario",
			minter: types.Minter{
				StartDate:           "2023-04-01",
				EndDate:             "2023-04-10",
				Denom:               "test",
				TotalMintAmount:     100,
				RemainingMintAmount: 60,
				LastMintDate:        "2023-04-04",
			},
			currentTime:    time.Date(2023, 4, 5, 0, 0, 0, 0, time.UTC),
			expectedAmount: 12,
		},
		{
			name: "Don't mint on the same day",
			minter: types.Minter{
				StartDate:           "2023-04-01",
				EndDate:             "2023-04-10",
				Denom:               "test",
				TotalMintAmount:     100,
				RemainingMintAmount: 60,
				LastMintDate:        "2023-04-05",
			},
			currentTime:    time.Date(2023, 4, 5, 0, 0, 0, 0, time.UTC),
			expectedAmount: 0,
		},
		{
			name: "No days left but remaining mint amount",
			minter: types.Minter{
				StartDate:           "2023-04-01",
				EndDate:             "2023-04-10",
				Denom:               "test",
				TotalMintAmount:     100,
				RemainingMintAmount: 60,
				LastMintDate:        "2023-04-11",
			},
			currentTime:    time.Date(2023, 4, 13, 0, 0, 0, 0, time.UTC),
			expectedAmount: 60,
		},
		{
			name: "Past end date",
			minter: types.Minter{
				StartDate:           "2023-04-01",
				EndDate:             "2023-04-10",
				Denom:               "test",
				TotalMintAmount:     100,
				RemainingMintAmount: 60,
				LastMintDate:        "2023-04-09",
			},
			currentTime:    time.Date(2023, 4, 11, 0, 0, 0, 0, time.UTC),
			expectedAmount: 60,
		},
		{
			name: "No remaining mint amount",
			minter: types.Minter{
				StartDate:           "2023-04-01",
				EndDate:             "2023-04-10",
				Denom:               "test",
				TotalMintAmount:     100,
				RemainingMintAmount: 0,
				LastMintDate:        "2023-04-05",
			},
			currentTime:    time.Date(2023, 4, 5, 0, 0, 0, 0, time.UTC),
			expectedAmount: 0,
		},
		{
			name: "Not yet started",
			minter: types.NewMinter(
				"2023-04-01",
				"2023-04-10",
				"test",
				100,
			),
			currentTime:    time.Date(2023, 4, 0, 0, 0, 0, 0, time.UTC),
			expectedAmount: 0,
		},
		{
			name: "First day",
			minter: types.NewMinter(
				"2023-04-01",
				"2023-04-10",
				"test",
				100,
			),
			currentTime:    time.Date(2023, 4, 1, 0, 0, 0, 0, time.UTC),
			expectedAmount: 11,
		},
		{
			name: "One day mint",
			minter: types.NewMinter(
				"2023-04-01",
				"2023-04-01",
				"test",
				100,
			),
			currentTime:    time.Date(2023, 4, 1, 0, 0, 0, 0, time.UTC),
			expectedAmount: 100,
		},
		{
			name: "One day mint - alreaddy minted",
			minter: types.Minter{
				StartDate:           "2023-04-01",
				EndDate:             "2023-04-01",
				Denom:               "test",
				TotalMintAmount:     100,
				RemainingMintAmount: 0,
				LastMintAmount:      100,
				LastMintDate:        "2023-04-01",
				LastMintHeight:      0,
			},
			currentTime:    time.Date(2023, 4, 1, 0, 1, 0, 0, time.UTC),
			expectedAmount: 0,
		},
		{
			name:           "No minter",
			minter:         types.InitialMinter(),
			currentTime:    time.Date(2023, 4, 5, 0, 0, 0, 0, time.UTC),
			expectedAmount: 0,
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			releaseAmount := tc.minter.GetReleaseAmountToday(tc.currentTime.UTC()).AmountOf(tc.minter.Denom).Uint64()
			if releaseAmount != tc.expectedAmount {
				t.Errorf("Expected release amount to be %d, but got %d", tc.expectedAmount, releaseAmount)
			}
		})
	}
}

func TestGetNumberOfDaysLeft(t *testing.T) {
	t.Parallel()
	testCases := []struct {
		name             string
		minter           types.Minter
		expectedDaysLeft uint64
		currentTime      time.Time
	}{
		{
			name: "Regular scenario",
			minter: types.Minter{
				StartDate:           "2023-04-01",
				EndDate:             "2023-04-10",
				Denom:               "test",
				TotalMintAmount:     100,
				RemainingMintAmount: 60,
				LastMintDate:        "2023-04-05",
			},
			currentTime:      time.Date(2023, 4, 5, 0, 0, 0, 0, time.UTC),
			expectedDaysLeft: 5,
		},
		{
			name: "No days left but amount left",
			minter: types.Minter{
				StartDate:           "2023-04-01",
				EndDate:             "2023-04-10",
				Denom:               "test",
				TotalMintAmount:     100,
				RemainingMintAmount: 60,
				LastMintDate:        "2023-04-10",
			},
			currentTime:      time.Date(2023, 4, 10, 0, 0, 0, 0, time.UTC),
			expectedDaysLeft: 0,
		},
		{
			name: "Past end date",
			minter: types.Minter{
				StartDate:           "2023-04-01",
				EndDate:             "2023-04-10",
				Denom:               "test",
				TotalMintAmount:     100,
				RemainingMintAmount: 60,
				LastMintDate:        "2023-04-09",
			},
			currentTime:      time.Date(2023, 4, 9, 0, 0, 0, 0, time.UTC),
			expectedDaysLeft: 1,
		},
		{
			name: "Regular end date",
			minter: types.NewMinter(
				"2023-04-24",
				"2023-05-19",
				"test",
				100,
			),
			expectedDaysLeft: 25,
			currentTime:      time.Date(2023, 4, 24, 0, 0, 0, 0, time.UTC),
		},
		{
			name: "No remaining mint amount",
			minter: types.Minter{
				StartDate:           "2023-04-01",
				EndDate:             "2023-04-10",
				Denom:               "test",
				TotalMintAmount:     100,
				RemainingMintAmount: 0,
				LastMintDate:        "2023-04-05",
			},
			currentTime:      time.Date(2023, 4, 5, 0, 0, 0, 0, time.UTC),
			expectedDaysLeft: 5,
		},
		{
			name: "First mint",
			minter: types.NewMinter(
				"2023-04-01",
				"2023-04-10",
				"test",
				100,
			),
			currentTime:      time.Date(2023, 4, 1, 0, 0, 0, 0, time.UTC),
			expectedDaysLeft: 9,
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			daysLeft := tc.minter.GetNumberOfDaysLeft(tc.currentTime)
			if daysLeft != tc.expectedDaysLeft {
				t.Errorf("Expected days left to be %d, but got %d", tc.expectedDaysLeft, daysLeft)
			}
		})
	}
}

func TestNewMinter(t *testing.T) {
	m := types.NewMinter(
		time.Now().Format(types.TokenReleaseDateFormat),
		time.Now().AddDate(0, 0, 1).Format(types.TokenReleaseDateFormat),
		sdk.DefaultBondDenom,
		1000,
	)
	require.Equal(t, m.TotalMintAmount, m.RemainingMintAmount)
}

func TestInitialMinter(t *testing.T) {
	m := types.InitialMinter()
	require.Equal(t, uint64(0), m.TotalMintAmount)
	require.Equal(t, time.Time{}.Format(types.TokenReleaseDateFormat), m.StartDate)
	require.Equal(t, time.Time{}.Format(types.TokenReleaseDateFormat), m.EndDate)
}

func TestDefaultInitialMinter(t *testing.T) {
	m := types.DefaultInitialMinter()
	require.Equal(t, uint64(0), m.TotalMintAmount)
	require.Equal(t, time.Time{}.Format(types.TokenReleaseDateFormat), m.StartDate)
	require.Equal(t, time.Time{}.Format(types.TokenReleaseDateFormat), m.EndDate)
	require.False(t, m.OngoingRelease())
}

func TestValidateMinterBase(t *testing.T) {
	m := types.NewMinter(
		time.Now().Format(types.TokenReleaseDateFormat),
		time.Now().AddDate(0, 0, -1).Format(types.TokenReleaseDateFormat),
		sdk.DefaultBondDenom,
		1000,
	)
	err := types.ValidateMinter(m)
	require.NotNil(t, err)

	m = types.NewMinter(
		time.Now().Format(types.TokenReleaseDateFormat),
		time.Now().AddDate(0, 0, 1).Format(types.TokenReleaseDateFormat),
		"invalid denom",
		1000,
	)
	err = types.ValidateMinter(m)
	require.NotNil(t, err)

	m = types.NewMinter(
		time.Now().Format(types.TokenReleaseDateFormat),
		time.Now().AddDate(0, 0, 1).Format(types.TokenReleaseDateFormat),
		sdk.DefaultBondDenom,
		1000,
	)
	err = types.ValidateMinter(m)
	require.True(t, m.OngoingRelease())
	require.Nil(t, err)
}

func TestGetLastMintDateTime(t *testing.T) {
	m := types.InitialMinter()
	_, err := time.Parse(types.TokenReleaseDateFormat, m.GetLastMintDate())
	require.NoError(t, err)
}

func TestGetStartDateTime(t *testing.T) {
	m := types.InitialMinter()
	_, err := time.Parse(types.TokenReleaseDateFormat, m.GetStartDate())
	require.NoError(t, err)
}

func TestGetEndDateTime(t *testing.T) {
	m := types.InitialMinter()
	_, err := time.Parse(types.TokenReleaseDateFormat, m.GetEndDate())
	require.NoError(t, err)
}

func TestGetLastMintAmountCoin(t *testing.T) {
	m := types.InitialMinter()
	coin := m.GetLastMintAmountCoin()
	require.Equal(t, sdk.NewInt(int64(0)), coin.Amount)
	require.Equal(t, sdk.DefaultBondDenom, coin.Denom)
}

func TestRecordSuccessfulMint(t *testing.T) {
	minter := types.NewMinter(
		time.Now().Format(types.TokenReleaseDateFormat),
		time.Now().Add(time.Hour*24*10).Format(types.TokenReleaseDateFormat),
		sdk.DefaultBondDenom,
		1000,
	)
	app := app.Setup(false, false)
	ctx := app.BaseApp.NewContext(false, tmproto.Header{})
	currentTime := time.Now().UTC()

	epoch := epochTypes.Epoch{
		CurrentEpochStartTime: currentTime,
		CurrentEpochHeight:    100,
	}

	minter.RecordSuccessfulMint(ctx, epoch, 100)

	// Check results
	if minter.GetRemainingMintAmount() != 900 {
		t.Errorf("Remaining mint amount was incorrect, got: %d, want: %d.", minter.GetRemainingMintAmount(), 900)
	}
}

func TestValidateMinter(t *testing.T) {
	minter := types.NewMinter(
		time.Now().Format(types.TokenReleaseDateFormat),
		time.Now().Add(time.Hour*24*10).Format(types.TokenReleaseDateFormat),
		sdk.DefaultBondDenom,
		1000,
	)

	err := types.ValidateMinter(minter)
	if err != nil {
		t.Errorf("Expected valid minter, got error: %v", err)
	}

	// Create invalid minter
	minter = types.NewMinter(
		time.Now().Add(time.Hour*24*10).Format(types.TokenReleaseDateFormat), // start date is after end date
		time.Now().Format(types.TokenReleaseDateFormat),
		sdk.DefaultBondDenom,
		1000,
	)

	err = types.ValidateMinter(minter)
	if err == nil {
		t.Errorf("Expected error, got valid minter")
	}
}

func TestGetLastMintDateTimeBase(t *testing.T) {
	// Create minter object
	minter := types.NewMinter(
		time.Now().Format(types.TokenReleaseDateFormat),
		time.Now().Add(time.Hour*24*10).Format(types.TokenReleaseDateFormat),
		sdk.DefaultBondDenom,
		1000,
	)

	// Call the function
	date := minter.GetLastMintDateTime()

	// Check the result
	// It should be the zero time because we haven't minted anything yet
	if !date.IsZero() {
		t.Errorf("Expected zero time, got: %v", date)
	}
}
