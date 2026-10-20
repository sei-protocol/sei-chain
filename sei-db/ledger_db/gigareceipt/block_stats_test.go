package gigareceipt

import (
	"math/big"
	"testing"

	"github.com/sei-protocol/sei-chain/sei-db/ledger_db/receipt"

	"github.com/sei-protocol/sei-chain/x/evm/types"
	"github.com/stretchr/testify/require"
)

func rewardRecord(gasUsed uint64, reward int64) receipt.ReceiptRecord {
	r := &types.Receipt{GasUsed: gasUsed}
	return receipt.ReceiptRecord{Receipt: r, Reward: big.NewInt(reward)}
}

func noRewardRecord(gasUsed uint64) receipt.ReceiptRecord {
	return receipt.ReceiptRecord{Receipt: &types.Receipt{GasUsed: gasUsed}}
}

func TestComputeBlockStatsAggregatesGasAndReward(t *testing.T) {
	records := []receipt.ReceiptRecord{
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
	records := []receipt.ReceiptRecord{noRewardRecord(10), noRewardRecord(20)}
	stats := ComputeBlockStats(records, DefaultRewardPercentiles)

	require.Equal(t, uint64(30), stats.TotalGasUsed)
	require.Equal(t, uint32(2), stats.TxCount)
	require.Equal(t, uint64(0), stats.RewardGasUsed)
	require.Empty(t, stats.RewardPercentiles)
}

func TestComputeBlockStatsSkipsNilReceipts(t *testing.T) {
	records := []receipt.ReceiptRecord{{Receipt: nil}, rewardRecord(10, 100)}
	stats := ComputeBlockStats(records, DefaultRewardPercentiles)
	require.Equal(t, uint32(1), stats.TxCount)
	require.Equal(t, uint64(10), stats.TotalGasUsed)
}

func TestEncodeDecodeBlockStatsRoundTrip(t *testing.T) {
	cases := []struct {
		name  string
		stats receipt.BlockStats
	}{
		{
			name: "typical",
			stats: receipt.BlockStats{
				TotalGasUsed:  1234,
				TxCount:       7,
				RewardGasUsed: 900,
				RewardPercentiles: []receipt.RewardPercentile{
					{Percentile: 0, Reward: 10},
					{Percentile: 50, Reward: 20},
					{Percentile: 100, Reward: 30},
				},
			},
		},
		{
			name:  "no reward-eligible tx",
			stats: receipt.BlockStats{TotalGasUsed: 500, TxCount: 3},
		},
		{
			name:  "empty block",
			stats: receipt.BlockStats{},
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
	valid := encodeBlockStats(receipt.BlockStats{
		TotalGasUsed:      1,
		RewardPercentiles: []receipt.RewardPercentile{{Percentile: 50, Reward: 1}},
	})

	_, err := decodeBlockStats(nil)
	require.Error(t, err)

	_, err = decodeBlockStats(valid[:len(valid)-1])
	require.Error(t, err)

	_, err = decodeBlockStats(append(valid, 0x00))
	require.Error(t, err)
}
