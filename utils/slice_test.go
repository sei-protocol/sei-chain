package utils_test

import (
	"testing"

	"github.com/sei-protocol/sei-chain/utils"
	"github.com/stretchr/testify/require"
)

func TestFilterUInt64Slice(t *testing.T) {
	s := []uint64{1, 2, 3}
	toBeFiltered := 2
	filteredS := utils.FilterUInt64Slice(s, uint64(toBeFiltered))
	require.Equal(t, []uint64{1, 3}, filteredS)
}

func TestMap(t *testing.T) {
	s := []int{1, 2, 3}
	mapped := utils.Map(s, func(i int) int { return i + 1 })
	require.Equal(t, 3, len(s))
	require.Equal(t, 2, mapped[0])
	require.Equal(t, 3, mapped[1])
	require.Equal(t, 4, mapped[2])
}

func TestSliceCopy(t *testing.T) {
	slice := []int{1}
	copy := utils.SliceCopy(slice)
	copy[0] = 2
	require.Equal(t, 1, slice[0])
}
