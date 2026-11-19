package gigareceipt

import (
	"encoding/binary"
	"fmt"
	"math"
	"math/big"
	"sort"

	"github.com/sei-protocol/sei-chain/sei-db/ledger_db/receipt"
)

const (
	blockStatsKeyPrefix = 's'
	// percentileFixedPointLen and rewardLen size one encoded receipt.RewardPercentile entry.
	percentileFixedPointLen = 4
	rewardLen               = 8
	// baseFeeLen sizes an encoded receipt.BlockStats.BaseFee.
	baseFeeLen = 32
)

// DefaultRewardPercentiles is applied when ReceiptStoreConfig.RewardPercentiles is empty, and by
// MemoryReceiptStore (which has no config). It covers the percentile sets real eth_feeHistory
// callers ask for: MetaMask ([50]), older MetaMask ([20,30,50]), ethers/alloy-style ([20,50,90]),
// adaptive pricing ([25,50,75,90]), and explorer/dashboard views ([25,50,75]). It covers the
// percentile sets real eth_feeHistory callers ask for: MetaMask ([50]), older MetaMask
// ([20,30,50]), ethers/alloy-style ([20,50,90]), adaptive pricing ([25,50,75,90]), and
// explorer/dashboard views ([25,50,75]).
var DefaultRewardPercentiles = []float64{0, 10, 20, 25, 30, 50, 75, 90, 100}

func roundPercentile(p float64) int32 {
	return int32(math.Round(p * 100)) //nolint:gosec // percentiles are bounded to [0,100]
}

type rewardEntry struct {
	reward  *big.Int
	gasUsed uint64
}

// ComputeBlockStats aggregates one block's receipts into TotalGasUsed, TxCount, and a
// gas-weighted reward-percentile walk over records with a non-nil Reward.
func ComputeBlockStats(records []receipt.ReceiptRecord, percentiles []float64) receipt.BlockStats {
	var stats receipt.BlockStats
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

	stats.RewardPercentiles = make([]receipt.RewardPercentile, 0, len(sortedPercentiles))
	var txIndex int
	sumGasUsed := entries[0].gasUsed
	for _, p := range sortedPercentiles {
		threshold := uint64(float64(stats.RewardGasUsed) * p / 100)
		for sumGasUsed < threshold && txIndex < len(entries)-1 {
			txIndex++
			sumGasUsed += entries[txIndex].gasUsed
		}
		stats.RewardPercentiles = append(stats.RewardPercentiles, receipt.RewardPercentile{
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
//	  count * (percentile*100 rounded, 4 BE signed + reward, 8 BE) +
//	  BaseFee (32 BE), present only when stats has one
//
// A BaseFee must pass checkBlockBaseFee.
func encodeBlockStats(stats receipt.BlockStats) []byte {
	size := 8 + 4 + 8 + 2 + len(stats.RewardPercentiles)*(percentileFixedPointLen+rewardLen)
	if stats.BaseFee != nil {
		size += baseFeeLen
	}
	buf := make([]byte, size)
	off := 0
	binary.BigEndian.PutUint64(buf[off:], stats.TotalGasUsed)
	off += 8
	binary.BigEndian.PutUint32(buf[off:], stats.TxCount)
	off += 4
	binary.BigEndian.PutUint64(buf[off:], stats.RewardGasUsed)
	off += 8
	//nolint:gosec // bounded by percentile config, never near 65536
	percentileCount := uint16(len(stats.RewardPercentiles))
	binary.BigEndian.PutUint16(buf[off:], percentileCount)
	off += 2
	for _, rp := range stats.RewardPercentiles {
		//nolint:gosec // percentiles are bounded to [0,100]
		fixedPoint := uint32(roundPercentile(rp.Percentile))
		binary.BigEndian.PutUint32(buf[off:], fixedPoint)
		off += percentileFixedPointLen
		binary.BigEndian.PutUint64(buf[off:], rp.Reward)
		off += rewardLen
	}
	if stats.BaseFee != nil {
		stats.BaseFee.FillBytes(buf[off : off+baseFeeLen])
	}
	return buf
}

// checkBlockBaseFee requires a block's base fee, when it has one, to be a non-negative value that
// fits the 32 bytes encodeBlockStats gives it.
func checkBlockBaseFee(baseFee *big.Int) error {
	if baseFee != nil && (baseFee.Sign() < 0 || baseFee.BitLen() > 8*baseFeeLen) {
		return fmt.Errorf("block base fee %s is not a 256-bit unsigned value", baseFee)
	}
	return nil
}

func decodeBlockStats(bz []byte) (receipt.BlockStats, error) {
	const headerLen = 8 + 4 + 8 + 2
	if len(bz) < headerLen {
		return receipt.BlockStats{}, fmt.Errorf("block stats value too short: %d bytes", len(bz))
	}
	var stats receipt.BlockStats
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
	withoutBaseFee := headerLen + int(count)*entryLen
	switch len(bz) {
	case withoutBaseFee:
	case withoutBaseFee + baseFeeLen:
		stats.BaseFee = new(big.Int).SetBytes(bz[withoutBaseFee:])
	default:
		return receipt.BlockStats{}, fmt.Errorf("block stats value has %d bytes, want %d or %d for %d percentiles",
			len(bz), withoutBaseFee, withoutBaseFee+baseFeeLen, count)
	}
	if count > 0 {
		stats.RewardPercentiles = make([]receipt.RewardPercentile, count)
		for i := range stats.RewardPercentiles {
			//nolint:gosec // round-trips a value this package encoded
			fixedPoint := int32(binary.BigEndian.Uint32(bz[off:]))
			off += percentileFixedPointLen
			reward := binary.BigEndian.Uint64(bz[off:])
			off += rewardLen
			stats.RewardPercentiles[i] = receipt.RewardPercentile{
				Percentile: float64(fixedPoint) / 100,
				Reward:     reward,
			}
		}
	}
	return stats, nil
}
