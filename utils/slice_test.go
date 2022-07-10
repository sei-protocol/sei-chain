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
