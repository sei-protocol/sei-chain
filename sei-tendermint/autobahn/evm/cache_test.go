package evm

import (
	"testing"

	"github.com/sei-protocol/sei-chain/sei-tendermint/libs/utils"
	"github.com/sei-protocol/sei-chain/sei-tendermint/libs/utils/require"
)

func TestCacheKeepsRecentEntriesAcrossRollover(t *testing.T) {
	valueOf := func(i int) []int { return []int{i} }
	const capacity = 4
	cache := newCache[int, []int](capacity)
	for i := range 3*capacity + 1 {
		cache.push(i, valueOf(i))
	}

	require.Equal(t, utils.None[[]int](), cache.get(capacity))
	require.Equal(t, utils.Some(valueOf(2*capacity)), cache.get(2*capacity))
	require.Equal(t, utils.Some(valueOf(3*capacity)), cache.get(3*capacity))
	// The same key can be present in both generations after another insertion.
	cache.push(2*capacity, valueOf(2*capacity))
	cache.pop(2 * capacity)
	cache.pop(3 * capacity)
	require.Equal(t, utils.None[[]int](), cache.get(3*capacity))
	require.Equal(t, utils.None[[]int](), cache.get(2*capacity))
}
