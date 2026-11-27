package im_test

import (
	"slices"
	"testing"

	"github.com/sei-protocol/sei-chain/sei-tendermint/libs/utils/im"
	"github.com/sei-protocol/sei-chain/sei-tendermint/libs/utils/require"
)

func TestSlice(t *testing.T) {
	var empty im.Slice[int]
	require.Equal(t, 0, empty.Len())
	require.Equal(t, []int(nil), slices.Collect(empty.All()))
	require.Panics(t, func() { empty.At(0) })

	s := im.NewSlice([]int{1, 2, 3})
	require.Equal(t, 3, s.Len())
	for i := range s.Len() {
		require.Equal(t, i+1, s.At(i))
	}
	require.Equal(t, []int{1, 2, 3}, slices.Collect(s.All()))
	require.Panics(t, func() { s.At(-1) })
	require.Panics(t, func() { s.At(s.Len()) })
}
