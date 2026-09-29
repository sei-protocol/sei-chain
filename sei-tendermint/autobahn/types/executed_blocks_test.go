package types

import (
	"testing"

	"github.com/sei-protocol/sei-chain/sei-tendermint/libs/utils/require"
)

func TestExecutedBlocks(t *testing.T) {
	var empty ExecutedBlocks
	require.Equal(t, ExecutedBlock{}, empty.Latest())
	_, ok := empty.Get(0)
	require.False(t, ok)

	w := NewExecutedBlocks(ExecutedBlock{Number: 10})
	for n := GlobalBlockNumber(11); n <= 10+ExecutedBlocksWindow; n++ {
		w = w.Push(ExecutedBlock{Number: n, GasUsed: uint64(n) * 1000})
	}
	require.Equal(t, ExecutedBlock{Number: 10 + ExecutedBlocksWindow, GasUsed: (10 + ExecutedBlocksWindow) * 1000}, w.Latest())
	// Block 10 was the oldest of ExecutedBlocksWindow+1 blocks and is evicted.
	_, ok = w.Get(10)
	require.False(t, ok)
	got, ok := w.Get(11)
	require.True(t, ok)
	require.Equal(t, ExecutedBlock{Number: 11, GasUsed: 11_000}, got)
	_, ok = w.Get(11 + ExecutedBlocksWindow)
	require.False(t, ok)
}
