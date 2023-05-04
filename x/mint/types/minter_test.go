package types

import (
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestParamsUsei(t *testing.T) {
	params := DefaultParams()
	err := params.Validate()
	require.Nil(t, err)

	params.MintDenom = "sei"
	err = params.Validate()
	require.NotNil(t, err)
}

func TestDaysBetween(t *testing.T) {
	t.Parallel()
	testCases := []struct {
		name      string
		date1     string
		date2     string
		expected  uint64
	}{
		{
			name:      "Same day",
			date1:     "2023-04-20T00:00:00Z",
			date2:     "2023-04-20T23:59:59Z",
			expected:  0,
		},
		{
			name:      "25 days apart",
			date1:     "2023-04-24T00:00:00Z",
			date2:     "2023-05-19T00:00:00Z",
			expected:  25,
		},
		{
			name:      "One day apart",
			date1:     "2023-04-20T00:00:00Z",
			date2:     "2023-04-21T00:00:00Z",
			expected:  1,
		},
		{
			name:      "Five days apart",
			date1:     "2023-04-20T00:00:00Z",
			date2:     "2023-04-25T00:00:00Z",
			expected:  5,
		},
		{
			name:      "Inverted dates",
			date1:     "2023-04-25T00:00:00Z",
			date2:     "2023-04-20T00:00:00Z",
			expected:  5,
		},
		{
			name:      "Less than 24 hours apart, crossing day boundary",
			date1:     "2023-04-20T23:00:00Z",
			date2:     "2023-04-21T22:59:59Z",
			expected:  1,
		},
		{
			name:      "Exactly 24 hours apart",
			date1:     "2023-04-20T12:34:56Z",
			date2:     "2023-04-21T12:34:56Z",
			expected:  1,
		},
		{
			name:      "One minute less than 24 hours apart",
			date1:     "2023-04-20T12:34:56Z",
			date2:     "2023-04-21T12:33:56Z",
			expected:  1,
		},
		{
			name:      "Inverted dates with times",
			date1:     "2023-04-25T15:30:00Z",
			date2:     "2023-04-20T10:00:00Z",
			expected:  5,
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			date1, _ := time.Parse(time.RFC3339, tc.date1)
			date2, _ := time.Parse(time.RFC3339, tc.date2)

			result := daysBetween(date1, date2)

			if result != tc.expected {
				t.Errorf("Expected days between %s and %s to be %d, but got %d", tc.date1, tc.date2, tc.expected, result)
			}
		})
	}
}

func TestGetReleaseAmountToday(t *testing.T) {
	t.Parallel()
	testCases := []struct {
		name          string
		minter        Minter
		currentTime   time.Time
		expectedAmount uint64
	}{
		{
			name: "Regular scenario",
			minter: Minter{
				StartDate:           "2023-04-01",
				EndDate:             "2023-04-10",
				Denom:               "test",
				TotalMintAmount:     100,
				RemainingMintAmount: 60,
				LastMintDate:        "2023-04-04",
			},
			currentTime:   time.Date(2023, 4, 5, 0, 0, 0, 0, time.UTC),
			expectedAmount: 12,
		},
		{
			name: "Don't mint on the same day",
			minter: Minter{
				StartDate:           "2023-04-01",
				EndDate:             "2023-04-10",
				Denom:               "test",
				TotalMintAmount:     100,
				RemainingMintAmount: 60,
				LastMintDate:        "2023-04-05",
			},
			currentTime:   time.Date(2023, 4, 5, 0, 0, 0, 0, time.UTC),
			expectedAmount: 0,
		},
		{
			name: "No days left but remaining mint amount",
			minter: Minter{
				StartDate:           "2023-04-01",
				EndDate:             "2023-04-10",
				Denom:               "test",
				TotalMintAmount:     100,
				RemainingMintAmount: 60,
				LastMintDate:        "2023-04-11",
			},
			currentTime:   time.Date(2023, 4, 13, 0, 0, 0, 0, time.UTC),
			expectedAmount: 60,
		},
		{
			name: "Past end date",
			minter: Minter{
				StartDate:           "2023-04-01",
				EndDate:             "2023-04-10",
				Denom:               "test",
				TotalMintAmount:     100,
				RemainingMintAmount: 60,
				LastMintDate:        "2023-04-09",
			},
			currentTime:   time.Date(2023, 4, 11, 0, 0, 0, 0, time.UTC),
			expectedAmount: 60,
		},
		{
			name: "No remaining mint amount",
			minter: Minter{
				StartDate:           "2023-04-01",
				EndDate:             "2023-04-10",
				Denom:               "test",
				TotalMintAmount:     100,
				RemainingMintAmount: 0,
				LastMintDate:        "2023-04-05",
			},
			currentTime:   time.Date(2023, 4, 5, 0, 0, 0, 0, time.UTC),
			expectedAmount: 0,
		},
		{
			name: "Not yet started",
			minter: NewMinter(
				"2023-04-01",
				"2023-04-10",
				"test",
				100,
			),
			currentTime:  time.Date(2023, 4, 0, 0, 0, 0, 0, time.UTC),
			expectedAmount: 0,
		},
		{
			name: "First day",
			minter: NewMinter(
				"2023-04-01",
				"2023-04-10",
				"test",
				100,
			),
			currentTime:  time.Date(2023, 4, 1, 0, 0, 0, 0, time.UTC),
			expectedAmount: 11,
		},
		{
			name: "One day mint",
			minter: NewMinter(
				"2023-04-01",
				"2023-04-01",
				"test",
				100,
			),
			currentTime:  time.Date(2023, 4, 1, 0, 0, 0, 0, time.UTC),
			expectedAmount: 100,
		},
		{
			name: "One day mint - alreaddy minted",
			minter: Minter{
				StartDate: "2023-04-01",
				EndDate: "2023-04-01",
				Denom: "test",
				TotalMintAmount: 100,
				RemainingMintAmount: 0,
				LastMintAmount: 100,
				LastMintDate: "2023-04-01",
				LastMintHeight: 0,
			},
			currentTime:  time.Date(2023, 4, 1, 0, 1, 0, 0, time.UTC),
			expectedAmount: 0,
		},
		{
			name: "No minter",
			minter: InitialMinter(),
			currentTime:   time.Date(2023, 4, 5, 0, 0, 0, 0, time.UTC),
			expectedAmount: 0,
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			releaseAmount := tc.minter.getReleaseAmountToday(tc.currentTime.UTC())
			if releaseAmount != tc.expectedAmount {
				t.Errorf("Expected release amount to be %d, but got %d", tc.expectedAmount, releaseAmount)
			}
		})
	}
}

func TestGetNumberOfDaysLeft(t *testing.T) {
	t.Parallel()
	testCases := []struct {
		name            string
		minter          Minter
		expectedDaysLeft uint64
		currentTime time.Time
	}{
		{
			name: "Regular scenario",
			minter: Minter{
				StartDate:			 "2023-04-01",
				EndDate:             "2023-04-10",
				Denom:               "test",
				TotalMintAmount:     100,
				RemainingMintAmount: 60,
				LastMintDate:        "2023-04-05",
			},
			currentTime:  time.Date(2023, 4, 5, 0, 0, 0, 0, time.UTC),
			expectedDaysLeft: 5,
		},
		{
			name: "No days left but amount left",
			minter: Minter{
				StartDate:           "2023-04-01",
				EndDate:             "2023-04-10",
				Denom:               "test",
				TotalMintAmount:     100,
				RemainingMintAmount: 60,
				LastMintDate:        "2023-04-10",
			},
			currentTime:  time.Date(2023, 4, 10, 0, 0, 0, 0, time.UTC),
			expectedDaysLeft: 0,
		},
		{
			name: "Past end date",
			minter: Minter{
				StartDate:           "2023-04-01",
				EndDate:             "2023-04-10",
				Denom:               "test",
				TotalMintAmount:     100,
				RemainingMintAmount: 60,
				LastMintDate:        "2023-04-09",
			},
			currentTime:  time.Date(2023, 4, 9, 0, 0, 0, 0, time.UTC),
			expectedDaysLeft: 1,
		},
		{
			name: "Regular end date",
			minter: NewMinter(
				"2023-04-24",
				"2023-05-19",
				"test",
				100,
			),
			expectedDaysLeft: 25,
			currentTime:  time.Date(2023, 4, 24, 0, 0, 0, 0, time.UTC),
		},
		{
			name: "No remaining mint amount",
			minter: Minter{
				StartDate:           "2023-04-01",
				EndDate:             "2023-04-10",
				Denom:               "test",
				TotalMintAmount:     100,
				RemainingMintAmount: 0,
				LastMintDate:        "2023-04-05",
			},
			currentTime:  time.Date(2023, 4, 5, 0, 0, 0, 0, time.UTC),
			expectedDaysLeft: 5,
		},
		{
			name: "First mint",
			minter: NewMinter(
				"2023-04-01",
				"2023-04-10",
				"test",
				100,
			),
			currentTime:  time.Date(2023, 4, 1, 0, 0, 0, 0, time.UTC),
			expectedDaysLeft: 9,
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			daysLeft := tc.minter.getNumberOfDaysLeft(tc.currentTime)
			if daysLeft != tc.expectedDaysLeft {
				t.Errorf("Expected days left to be %d, but got %d", tc.expectedDaysLeft, daysLeft)
			}
		})
	}
}
