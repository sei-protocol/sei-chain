package littidx

import (
	"math/big"
	"testing"

	receipttypes "github.com/sei-protocol/sei-chain/sei-db/receipt_db/types"
	"github.com/stretchr/testify/require"
)

func TestEncodeDecodeBlockStatsRoundTrip(t *testing.T) {
	cases := []struct {
		name  string
		stats receipttypes.BlockStats
	}{
		{
			name: "typical",
			stats: receipttypes.BlockStats{
				TotalGasUsed:  1234,
				TxCount:       7,
				RewardGasUsed: 900,
				RewardPercentiles: []receipttypes.RewardPercentile{
					{Percentile: 0, Reward: 10},
					{Percentile: 50, Reward: 20},
					{Percentile: 100, Reward: 30},
				},
			},
		},
		{
			name:  "no reward-eligible tx",
			stats: receipttypes.BlockStats{TotalGasUsed: 500, TxCount: 3},
		},
		{
			name:  "empty block",
			stats: receipttypes.BlockStats{},
		},
		{
			name: "with a base fee",
			stats: receipttypes.BlockStats{
				TotalGasUsed:      21_000,
				TxCount:           1,
				RewardGasUsed:     21_000,
				RewardPercentiles: []receipttypes.RewardPercentile{{Percentile: 50, Reward: 5}},
				BaseFee:           big.NewInt(1_000_000_000),
			},
		},
		{
			name:  "empty block with the largest base fee",
			stats: receipttypes.BlockStats{BaseFee: new(big.Int).Sub(new(big.Int).Lsh(big.NewInt(1), 256), big.NewInt(1))},
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
			require.Equal(t, tc.stats.BaseFee.String(), got.BaseFee.String())
		})
	}
}

// TestDecodeBlockStatsRejectsMalformedInput is what GetBlockStats relies on to fall back safely:
// a short or wrong-length blob must error rather than silently decode into a wrong value.
func TestDecodeBlockStatsRejectsMalformedInput(t *testing.T) {
	valid := encodeBlockStats(receipttypes.BlockStats{
		TotalGasUsed:      1,
		RewardPercentiles: []receipttypes.RewardPercentile{{Percentile: 50, Reward: 1}},
	})

	_, err := decodeBlockStats(nil)
	require.Error(t, err)

	_, err = decodeBlockStats(valid[:len(valid)-1])
	require.Error(t, err)

	_, err = decodeBlockStats(append(valid, 0x00))
	require.Error(t, err)

	withBaseFee := encodeBlockStats(receipttypes.BlockStats{BaseFee: big.NewInt(1)})
	_, err = decodeBlockStats(withBaseFee[:len(withBaseFee)-1])
	require.Error(t, err)
}

func TestCheckBlockBaseFee(t *testing.T) {
	require.NoError(t, checkBlockBaseFee(nil))
	require.NoError(t, checkBlockBaseFee(new(big.Int)))
	require.NoError(t, checkBlockBaseFee(new(big.Int).Sub(new(big.Int).Lsh(big.NewInt(1), 256), big.NewInt(1))))
	require.Error(t, checkBlockBaseFee(big.NewInt(-1)))
	require.Error(t, checkBlockBaseFee(new(big.Int).Lsh(big.NewInt(1), 256)))
}
