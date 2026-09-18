package receipt

import (
	"math/big"
	"testing"

	"github.com/sei-protocol/sei-chain/x/evm/types"
	"github.com/stretchr/testify/require"
)

func rewardRecord(gasUsed uint64, reward int64) ReceiptRecord {
	r := &types.Receipt{GasUsed: gasUsed}
	return ReceiptRecord{Receipt: r, Reward: big.NewInt(reward)}
}

func noRewardRecord(gasUsed uint64) ReceiptRecord {
	return ReceiptRecord{Receipt: &types.Receipt{GasUsed: gasUsed}}
}

func TestComputeBlockStatsAggregatesGasAndReward(t *testing.T) {
	records := []ReceiptRecord{
		rewardRecord(10, 100),
		rewardRecord(20, 300),
		noRewardRecord(5), // e.g. a failed-ante tx: counts toward TotalGasUsed, not toward reward
		rewardRecord(30, 200),
	}
	stats := ComputeBlockStats(records, []float64{0, 50, 100})

	require.Equal(t, uint64(10+20+5+30), stats.TotalGasUsed)
	require.Equal(t, uint32(4), stats.TxCount)
	require.Equal(t, uint64(10+20+30), stats.RewardGasUsed)
	require.Len(t, stats.RewardPercentiles, 3)

	min, ok := stats.RewardAt(0)
	require.True(t, ok)
	require.Equal(t, uint64(100), min)

	max, ok := stats.RewardAt(100)
	require.True(t, ok)
	require.Equal(t, uint64(300), max)
}

func TestComputeBlockStatsNoRewardEligibleRecords(t *testing.T) {
	records := []ReceiptRecord{noRewardRecord(10), noRewardRecord(20)}
	stats := ComputeBlockStats(records, DefaultRewardPercentiles)

	require.Equal(t, uint64(30), stats.TotalGasUsed)
	require.Equal(t, uint32(2), stats.TxCount)
	require.Equal(t, uint64(0), stats.RewardGasUsed)
	require.Empty(t, stats.RewardPercentiles)
}

func TestComputeBlockStatsSkipsNilReceipts(t *testing.T) {
	records := []ReceiptRecord{{Receipt: nil}, rewardRecord(10, 100)}
	stats := ComputeBlockStats(records, DefaultRewardPercentiles)
	require.Equal(t, uint32(1), stats.TxCount)
	require.Equal(t, uint64(10), stats.TotalGasUsed)
}

// TestBlockStatsRewardAtIsPerEntryNotPositional is the case a partially-stored or reconfigured
// percentile set depends on: each entry carries its own percentile, so a lookup for a value this
// block never stored is a clean miss — never a mismatched read against some other percentile's
// slot — regardless of how many entries are present or in what order.
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

func TestEncodeDecodeBlockStatsRoundTrip(t *testing.T) {
	cases := []struct {
		name  string
		stats BlockStats
	}{
		{
			name: "typical",
			stats: BlockStats{
				TotalGasUsed:  1234,
				TxCount:       7,
				RewardGasUsed: 900,
				RewardPercentiles: []RewardPercentile{
					{Percentile: 0, Reward: 10},
					{Percentile: 50, Reward: 20},
					{Percentile: 100, Reward: 30},
				},
			},
		},
		{
			name:  "no reward-eligible tx",
			stats: BlockStats{TotalGasUsed: 500, TxCount: 3},
		},
		{
			name:  "empty block",
			stats: BlockStats{},
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, err := decodeBlockStats(encodeBlockStats(tc.stats))
			require.NoError(t, err)
			require.Equal(t, tc.stats.TotalGasUsed, got.TotalGasUsed)
			require.Equal(t, tc.stats.TxCount, got.TxCount)
			require.Equal(t, tc.stats.RewardGasUsed, got.RewardGasUsed)
			require.Equal(t, tc.stats.RewardPercentiles, got.RewardPercentiles)
		})
	}
}

// TestDecodeBlockStatsRejectsMalformedInput is what GetBlockStats relies on to fall back safely:
// a short or wrong-length blob must error rather than silently decode into a wrong value.
func TestDecodeBlockStatsRejectsMalformedInput(t *testing.T) {
	valid := encodeBlockStats(BlockStats{
		TotalGasUsed:      1,
		RewardPercentiles: []RewardPercentile{{Percentile: 50, Reward: 1}},
	})

	_, err := decodeBlockStats(nil)
	require.Error(t, err)

	_, err = decodeBlockStats(valid[:len(valid)-1])
	require.Error(t, err)

	_, err = decodeBlockStats(append(valid, 0x00))
	require.Error(t, err)
}
