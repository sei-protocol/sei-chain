package metrics

import (
	"math/big"
	"sort"

	receipttypes "github.com/sei-protocol/sei-chain/sei-db/receipt_db/types"
)

// DefaultRewardPercentiles is the percentile set stored when ReceiptStoreConfig.RewardPercentiles is
// empty. It covers the sets real eth_feeHistory callers ask for: MetaMask ([50]), older MetaMask
// ([20,30,50]), ethers/alloy-style ([20,50,90]), adaptive pricing ([25,50,75,90]), and
// explorer/dashboard views ([25,50,75]).
var DefaultRewardPercentiles = []float64{0, 10, 20, 25, 30, 50, 75, 90, 100}

type rewardEntry struct {
	reward  *big.Int
	gasUsed uint64
}

// ComputeBlockStats aggregates one block's receipts into TotalGasUsed, TxCount, and a
// gas-weighted reward-percentile walk over records with a non-nil Reward.
func ComputeBlockStats(records []receipttypes.ReceiptRecord, percentiles []float64) receipttypes.BlockStats {
	var stats receipttypes.BlockStats
	entries := make([]rewardEntry, 0, len(records))
	for _, r := range records {
		if r.Receipt == nil {
			continue
		}
		stats.TotalGasUsed += r.Receipt.GasUsed
		stats.TxCount++
		if r.Reward != nil {
			entries = append(entries, rewardEntry{reward: r.Reward, gasUsed: r.Receipt.GasUsed})
			stats.RewardGasUsed += r.Receipt.GasUsed
		}
	}
	if len(percentiles) == 0 || len(entries) == 0 {
		return stats
	}
	sort.SliceStable(entries, func(i, j int) bool {
		return entries[i].reward.Cmp(entries[j].reward) < 0
	})
	sortedPercentiles := append([]float64(nil), percentiles...)
	sort.Float64s(sortedPercentiles)

	stats.RewardPercentiles = make([]receipttypes.RewardPercentile, 0, len(sortedPercentiles))
	var txIndex int
	sumGasUsed := entries[0].gasUsed
	for _, p := range sortedPercentiles {
		threshold := uint64(float64(stats.RewardGasUsed) * p / 100)
		for sumGasUsed < threshold && txIndex < len(entries)-1 {
			txIndex++
			sumGasUsed += entries[txIndex].gasUsed
		}
		stats.RewardPercentiles = append(stats.RewardPercentiles, receipttypes.RewardPercentile{
			Percentile: p,
			Reward:     entries[txIndex].reward.Uint64(),
		})
	}
	return stats
}
