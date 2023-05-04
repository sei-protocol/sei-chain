package types

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestValidateTokenReleaseSchedule(t *testing.T) {
	t.Parallel()
	t.Run("valid release schedule", func(t *testing.T) {
		validSchedule := []ScheduledTokenRelease{
			{
				StartDate:          "2023-01-01",
				EndDate:            "2023-01-31",
				TokenReleaseAmount: 1000,
			},
			{
				StartDate:          "2023-02-01",
				EndDate:            "2023-02-28",
				TokenReleaseAmount: 2000,
			},
		}
		err := validateTokenReleaseSchedule(validSchedule)
		assert.Nil(t, err)
	})

	t.Run("invalid parameter type", func(t *testing.T) {
		invalidParam := "invalid"
		err := validateTokenReleaseSchedule(invalidParam)
		assert.NotNil(t, err)
		assert.Contains(t, err.Error(), "invalid parameter type")
	})

	t.Run("invalid start date format", func(t *testing.T) {
		invalidStartDate := []ScheduledTokenRelease{
			{
				StartDate:          "invalid",
				EndDate:            "2023-01-31",
				TokenReleaseAmount: 1000,
			},
		}
		err := validateTokenReleaseSchedule(invalidStartDate)
		assert.NotNil(t, err)
		assert.Contains(t, err.Error(), "invalid start date format")
	})

	t.Run("invalid end date format", func(t *testing.T) {
		invalidEndDate := []ScheduledTokenRelease{
			{
				StartDate:          "2023-01-01",
				EndDate:            "invalid",
				TokenReleaseAmount: 1000,
			},
		}
		err := validateTokenReleaseSchedule(invalidEndDate)
		assert.NotNil(t, err)
		assert.Contains(t, err.Error(), "invalid end date format")
	})

	t.Run("start date not before end date", func(t *testing.T) {
		invalidDateOrder := []ScheduledTokenRelease{
			{
				StartDate:          "2023-01-31",
				EndDate:            "2023-01-01",
				TokenReleaseAmount: 1000,
			},
		}
		err := validateTokenReleaseSchedule(invalidDateOrder)
		assert.NotNil(t, err)
		assert.Contains(t, err.Error(), "start date must be before end date")
	})

	t.Run("overlapping release period", func(t *testing.T) {
		overlappingPeriod := []ScheduledTokenRelease{
			{
				StartDate:          "2023-01-01",
				EndDate:            "2023-01-31",
				TokenReleaseAmount: 1000,
			},
			{
				StartDate:          "2023-01-15",
				EndDate:            "2023-01-31",
				TokenReleaseAmount: 2000,
			},
		}
		err := validateTokenReleaseSchedule(overlappingPeriod)
		assert.NotNil(t, err)
		assert.Contains(t, err.Error(), "overlapping release period detected")
	})
	t.Run("non-overlapping periods with different order", func(t *testing.T) {
		nonOverlappingPeriods := []ScheduledTokenRelease{
			{
				StartDate:          "2023-03-01",
				EndDate:            "2023-03-31",
				TokenReleaseAmount: 3000,
			},
			{
				StartDate:          "2023-01-01",
				EndDate:            "2023-01-31",
				TokenReleaseAmount: 1000,
			},
			{
				StartDate:          "2023-02-01",
				EndDate:            "2023-02-28",
				TokenReleaseAmount: 2000,
			},
		}
		err := validateTokenReleaseSchedule(nonOverlappingPeriods)
		assert.Nil(t, err)
	})

	t.Run("unsorted input with overlapping windows", func(t *testing.T) {
		unsortedOverlapping := []ScheduledTokenRelease{
			{
				StartDate:          "2023-03-01",
				EndDate:            "2023-03-31",
				TokenReleaseAmount: 3000,
			},
			{
				StartDate:          "2023-01-15",
				EndDate:            "2023-02-14",
				TokenReleaseAmount: 2000,
			},
			{
				StartDate:          "2023-01-01",
				EndDate:            "2023-01-31",
				TokenReleaseAmount: 1000,
			},
		}
		err := validateTokenReleaseSchedule(unsortedOverlapping)
		assert.NotNil(t, err)
		assert.Contains(t, err.Error(), "overlapping release period detected")
	})

	t.Run("end date equals start date of next period is fine", func(t *testing.T) {
		endEqualsStart := []ScheduledTokenRelease{
			{
				StartDate:          "2023-01-01",
				EndDate:            "2023-01-31",
				TokenReleaseAmount: 1000,
			},
			{
				StartDate:          "2023-01-31",
				EndDate:            "2023-02-28",
				TokenReleaseAmount: 2000,
			},
			{
				StartDate:          "2023-02-28",
				EndDate:            "2023-03-31",
				TokenReleaseAmount: 3000,
			},
		}
		err := validateTokenReleaseSchedule(endEqualsStart)
		assert.Nil(t, err)
	})
}
