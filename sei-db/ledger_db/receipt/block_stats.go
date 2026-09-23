package receipt

import (
	"encoding/binary"
	"fmt"
	"math"
	"math/big"
	"sort"
)

const (
	blockStatsKeyPrefix = 's'
	// percentileFixedPointLen and rewardLen size one encoded RewardPercentile entry.
	percentileFixedPointLen = 4
	rewardLen               = 8
)

// DefaultRewardPercentiles is applied when ReceiptStoreConfig.RewardPercentiles is empty, and by
// MemoryReceiptStore (which has no config). It covers the percentile sets real eth_feeHistory
// callers ask for: MetaMask ([50]), older MetaMask ([20,30,50]), ethers/alloy-style ([20,50,90]),
// adaptive pricing ([25,50,75,90]), and explorer/dashboard views ([25,50,75]). It covers the percentile sets real eth_feeHistory
// callers ask for: MetaMask ([50]), older MetaMask ([20,30,50]), ethers/alloy-style ([20,50,90]),
// adaptive pricing ([25,50,75,90]), and explorer/dashboard views ([25,50,75]).
var DefaultRewardPercentiles = []float64{0, 10, 20, 25, 30, 50, 75, 90, 100}

// RewardPercentile is one gas-weighted reward percentile computed at block-write time.
type RewardPercentile struct {
	// Percentile is matched against a request rounded to 2 decimal places; no interpolation.
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

// RewardAt returns the stored reward for percentile p and whether it was found.
func (s BlockStats) RewardAt(p float64) (uint64, bool) {
	target := roundPercentile(p)
	for _, rp := range s.RewardPercentiles {
		if roundPercentile(rp.Percentile) == target {
			return rp.Reward, true
		}
	}
	return 0, false
}

func roundPercentile(p float64) int32 {
	return int32(math.Round(p * 100)) //nolint:gosec // percentiles are bounded to [0,100]
}

type rewardEntry struct {
	reward  *big.Int
	gasUsed uint64
}

// ComputeBlockStats aggregates one block's receipts into TotalGasUsed, TxCount, and a
// gas-weighted reward-percentile walk over records with a non-nil Reward.
func ComputeBlockStats(records []ReceiptRecord, percentiles []float64) BlockStats {
	var stats BlockStats
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

	stats.RewardPercentiles = make([]RewardPercentile, 0, len(sortedPercentiles))
	var txIndex int
	sumGasUsed := entries[0].gasUsed
	for _, p := range sortedPercentiles {
		threshold := uint64(float64(stats.RewardGasUsed) * p / 100)
		for sumGasUsed < threshold && txIndex < len(entries)-1 {
			txIndex++
			sumGasUsed += entries[txIndex].gasUsed
		}
		stats.RewardPercentiles = append(stats.RewardPercentiles, RewardPercentile{
			Percentile: p,
			Reward:     entries[txIndex].reward.Uint64(),
		})
	}
	return stats
}

// blockStatsKey identifies blockNumber's stats entry in the pebble index, distinct from the 'm:'
// metadata keys and the 't' tag-index keys sharing that store.
func blockStatsKey(blockNumber uint64) []byte {
	key := make([]byte, 1+blockNumLen)
	key[0] = blockStatsKeyPrefix
	binary.BigEndian.PutUint64(key[1:], blockNumber)
	return key
}

// encodeBlockStats serializes stats as:
//
//	TotalGasUsed (8 BE) + TxCount (4 BE) + RewardGasUsed (8 BE) + count (2 BE) +
//	  count * (percentile*100 rounded, 4 BE signed + reward, 8 BE)
func encodeBlockStats(stats BlockStats) []byte {
	buf := make([]byte, 8+4+8+2+len(stats.RewardPercentiles)*(percentileFixedPointLen+rewardLen))
	off := 0
	binary.BigEndian.PutUint64(buf[off:], stats.TotalGasUsed)
	off += 8
	binary.BigEndian.PutUint32(buf[off:], stats.TxCount)
	off += 4
	binary.BigEndian.PutUint64(buf[off:], stats.RewardGasUsed)
	off += 8
	binary.BigEndian.PutUint16(buf[off:], uint16(len(stats.RewardPercentiles))) //nolint:gosec // bounded by percentile config, never near 65536
	off += 2
	for _, rp := range stats.RewardPercentiles {
		binary.BigEndian.PutUint32(buf[off:], uint32(roundPercentile(rp.Percentile))) //nolint:gosec // percentiles are bounded to [0,100]
		off += percentileFixedPointLen
		binary.BigEndian.PutUint64(buf[off:], rp.Reward)
		off += rewardLen
	}
	return buf
}

func decodeBlockStats(bz []byte) (BlockStats, error) {
	const headerLen = 8 + 4 + 8 + 2
	if len(bz) < headerLen {
		return BlockStats{}, fmt.Errorf("block stats value too short: %d bytes", len(bz))
	}
	var stats BlockStats
	off := 0
	stats.TotalGasUsed = binary.BigEndian.Uint64(bz[off:])
	off += 8
	stats.TxCount = binary.BigEndian.Uint32(bz[off:])
	off += 4
	stats.RewardGasUsed = binary.BigEndian.Uint64(bz[off:])
	off += 8
	count := binary.BigEndian.Uint16(bz[off:])
	off += 2
	entryLen := percentileFixedPointLen + rewardLen
	if len(bz) != headerLen+int(count)*entryLen {
		return BlockStats{}, fmt.Errorf("block stats value has %d bytes, want %d for %d percentiles", len(bz), headerLen+int(count)*entryLen, count)
	}
	if count > 0 {
		stats.RewardPercentiles = make([]RewardPercentile, count)
		for i := range stats.RewardPercentiles {
			fixedPoint := int32(binary.BigEndian.Uint32(bz[off:])) //nolint:gosec // round-trips a value this package encoded
			off += percentileFixedPointLen
			reward := binary.BigEndian.Uint64(bz[off:])
			off += rewardLen
			stats.RewardPercentiles[i] = RewardPercentile{Percentile: float64(fixedPoint) / 100, Reward: reward}
		}
	}
	return stats, nil
}
