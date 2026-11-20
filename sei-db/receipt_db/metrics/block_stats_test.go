package metrics

import (
	"math/big"
	"testing"

	receipttypes "github.com/sei-protocol/sei-chain/sei-db/receipt_db/types"
	"github.com/sei-protocol/sei-chain/x/evm/types"
	"github.com/stretchr/testify/require"
)

func rewardRecord(gasUsed uint64, reward int64) receipttypes.ReceiptRecord {
	r := &types.Receipt{GasUsed: gasUsed}
	return receipttypes.ReceiptRecord{Receipt: r, Reward: big.NewInt(reward)}
}

func noRewardRecord(gasUsed uint64) receipttypes.ReceiptRecord {
	return receipttypes.ReceiptRecord{Receipt: &types.Receipt{GasUsed: gasUsed}}
}

func TestComputeBlockStatsAggregatesGasAndReward(t *testing.T) {
	records := []receipttypes.ReceiptRecord{
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
	records := []receipttypes.ReceiptRecord{noRewardRecord(10), noRewardRecord(20)}
	stats := ComputeBlockStats(records, DefaultRewardPercentiles)

	require.Equal(t, uint64(30), stats.TotalGasUsed)
	require.Equal(t, uint32(2), stats.TxCount)
	require.Equal(t, uint64(0), stats.RewardGasUsed)
	require.Empty(t, stats.RewardPercentiles)
}

func TestComputeBlockStatsSkipsNilReceipts(t *testing.T) {
	records := []receipttypes.ReceiptRecord{{Receipt: nil}, rewardRecord(10, 100)}
	stats := ComputeBlockStats(records, DefaultRewardPercentiles)
	require.Equal(t, uint32(1), stats.TxCount)
	require.Equal(t, uint64(10), stats.TotalGasUsed)
}
