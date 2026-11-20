package littidx

import (
	"encoding/binary"
	"fmt"
	"math"

	receipttypes "github.com/sei-protocol/sei-chain/sei-db/receipt_db/types"
)

const (
	blockStatsKeyPrefix = 's'
	// percentileFixedPointLen and rewardLen size one encoded receipttypes.RewardPercentile entry.
	percentileFixedPointLen = 4
	rewardLen               = 8
)

func roundPercentile(p float64) int32 {
	return int32(math.Round(p * 100)) //nolint:gosec // percentiles are bounded to [0,100]
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
func encodeBlockStats(stats receipttypes.BlockStats) []byte {
	buf := make([]byte, 8+4+8+2+len(stats.RewardPercentiles)*(percentileFixedPointLen+rewardLen))
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
	return buf
}

func decodeBlockStats(bz []byte) (receipttypes.BlockStats, error) {
	const headerLen = 8 + 4 + 8 + 2
	if len(bz) < headerLen {
		return receipttypes.BlockStats{}, fmt.Errorf("block stats value too short: %d bytes", len(bz))
	}
	var stats receipttypes.BlockStats
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
		return receipttypes.BlockStats{}, fmt.Errorf("block stats value has %d bytes, want %d for %d percentiles",
			len(bz), headerLen+int(count)*entryLen, count)
	}
	if count > 0 {
		stats.RewardPercentiles = make([]receipttypes.RewardPercentile, count)
		for i := range stats.RewardPercentiles {
			//nolint:gosec // round-trips a value this package encoded
			fixedPoint := int32(binary.BigEndian.Uint32(bz[off:]))
			off += percentileFixedPointLen
			reward := binary.BigEndian.Uint64(bz[off:])
			off += rewardLen
			stats.RewardPercentiles[i] = receipttypes.RewardPercentile{
				Percentile: float64(fixedPoint) / 100,
				Reward:     reward,
			}
		}
	}
	return stats, nil
}
