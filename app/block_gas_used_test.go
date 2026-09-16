package app

import (
	"math"
	"testing"

	"github.com/sei-protocol/sei-chain/sei-tendermint/abci/types"
	"github.com/stretchr/testify/require"
)

// gasUsedResults builds one ExecTxResult per gas value, matching the shape
// FinalizeBlock hands to sumBlockGasUsed.
func gasUsedResults(gasUsed ...int64) []*types.ExecTxResult {
	results := make([]*types.ExecTxResult, 0, len(gasUsed))
	for _, g := range gasUsed {
		results = append(results, &types.ExecTxResult{GasUsed: g})
	}
	return results
}

// TestSumBlockGasUsedEmptyBlock records a zero total for a block with no transactions, so
// the histogram carries one sample per finalized block.
func TestSumBlockGasUsedEmptyBlock(t *testing.T) {
	total, ok := sumBlockGasUsed(nil)
	require.True(t, ok)
	require.Zero(t, total)
}

// TestSumBlockGasUsedSumsResults covers the ordinary path.
func TestSumBlockGasUsedSumsResults(t *testing.T) {
	total, ok := sumBlockGasUsed(gasUsedResults(21_000, 100_000, 0, 5_000_000))
	require.True(t, ok)
	require.Equal(t, int64(5_121_000), total)
}

// TestSumBlockGasUsedSkipsNilResults exercises the nil-entry branch of the accounting loop.
func TestSumBlockGasUsedSkipsNilResults(t *testing.T) {
	results := []*types.ExecTxResult{nil, {GasUsed: 21_000}, nil, {GasUsed: 42_000}}
	total, ok := sumBlockGasUsed(results)
	require.True(t, ok)
	require.Equal(t, int64(63_000), total)
}

// TestSumBlockGasUsedRejectsNegativeGas exercises the negative-gas branch.
func TestSumBlockGasUsedRejectsNegativeGas(t *testing.T) {
	_, ok := sumBlockGasUsed(gasUsedResults(21_000, -1))
	require.False(t, ok)
}

// TestSumBlockGasUsedRejectsOverflow exercises the int64 overflow branch.
func TestSumBlockGasUsedRejectsOverflow(t *testing.T) {
	_, ok := sumBlockGasUsed(gasUsedResults(math.MaxInt64, 1))
	require.False(t, ok)
}

// TestSumBlockGasUsedAcceptsExactInt64Max confirms the overflow guard is not off by one.
func TestSumBlockGasUsedAcceptsExactInt64Max(t *testing.T) {
	total, ok := sumBlockGasUsed(gasUsedResults(math.MaxInt64-1, 1))
	require.True(t, ok)
	require.Equal(t, int64(math.MaxInt64), total)
}
