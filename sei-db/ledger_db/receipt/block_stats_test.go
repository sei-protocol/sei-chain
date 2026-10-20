package receipt

import (
	"testing"

	"github.com/stretchr/testify/require"
)

// TestBlockStatsRewardAtIsPerEntryNotPositional verifies a percentile lookup matches by value,
// not position, so a miss is always a clean miss.
func TestBlockStatsRewardAtIsPerEntryNotPositional(t *testing.T) {
	stats := BlockStats{RewardPercentiles: []RewardPercentile{
		{Percentile: 25, Reward: 111},
		{Percentile: 75, Reward: 333},
	}}

	got, ok := stats.RewardAt(25)
	require.True(t, ok)
	require.Equal(t, uint64(111), got)

	got, ok = stats.RewardAt(75)
	require.True(t, ok)
	require.Equal(t, uint64(333), got)

	// 50 was never stored (e.g. config changed, or this block predates a wider percentile set) —
	// a miss, not a wrong value borrowed from a neighboring slot.
	_, ok = stats.RewardAt(50)
	require.False(t, ok)

	// An empty list (no reward-eligible tx at all) misses every request, never panics.
	require.Empty(t, BlockStats{}.RewardPercentiles)
	_, ok = BlockStats{}.RewardAt(50)
	require.False(t, ok)
}
