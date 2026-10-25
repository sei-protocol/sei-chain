package receipt

// RewardPercentile is one gas-weighted reward percentile computed at block-write time.
type RewardPercentile struct {
	// Percentile is matched exactly against a request; no rounding or interpolation.
	Percentile float64
	Reward     uint64
}

// BlockStats is the block-level aggregate recorded alongside a block's receipts.
type BlockStats struct {
	// TotalGasUsed is summed over every receipt in the block, for gasUsedRatio.
	TotalGasUsed uint64
	TxCount      uint32
	// RewardGasUsed is summed over only the reward-eligible receipts (see ReceiptRecord.Reward) —
	// the weighting denominator for RewardPercentiles.
	RewardGasUsed uint64
	// RewardPercentiles is sorted ascending by Percentile; empty if no receipt was reward-eligible.
	RewardPercentiles []RewardPercentile
}

// RewardAt returns the stored reward for percentile p and whether it was found, matching
// exactly: a near-miss (e.g. 50.004 against a stored 50) is a miss, not a match.
func (s BlockStats) RewardAt(p float64) (uint64, bool) {
	for _, rp := range s.RewardPercentiles {
		if rp.Percentile == p {
			return rp.Reward, true
		}
	}
	return 0, false
}
