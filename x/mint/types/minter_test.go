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
			expected:  0,
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
			expected:  0,
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
