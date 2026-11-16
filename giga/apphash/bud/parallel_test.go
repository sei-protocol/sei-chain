package bud

import (
	"bytes"
	"fmt"
	"math/rand"
	"runtime"
	"slices"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/sei-protocol/sei-chain/sei-db/state_db/giga/types/mutation"
	"github.com/sei-protocol/sei-chain/sei-db/state_db/sc/flatkv/vtype"
)

// useFourProcs sets GOMAXPROCS to 4 for the test, so steps run on several goroutines.
func useFourProcs(t *testing.T) {
	prev := runtime.GOMAXPROCS(4)
	t.Cleanup(func() { runtime.GOMAXPROCS(prev) })
}

// largeBlock returns n mutations with distinct random keys, in random order.
func largeBlock(rng *rand.Rand, n int) []mutation.Mutation {
	keys := make([]string, n)
	for i := range keys {
		keys[i] = fmt.Sprintf("%016x-%d", rng.Uint64(), i)
	}
	return insertsOf(keys...)
}

func TestSortMutationsByKeyMatchesASerialSort(t *testing.T) {
	useFourProcs(t)
	rng := rand.New(rand.NewSource(1))
	for _, n := range []int{0, 1, 2, 3, 100, 20011} {
		got := largeBlock(rng, n)
		want := slices.Clone(got)
		slices.SortFunc(want, func(a mutation.Mutation, b mutation.Mutation) int { return bytes.Compare(a.Key, b.Key) })
		sortMutationsByKey(got)
		require.Equal(t, want, got, "n=%d", n)
	}
}

func TestBUDLeafHashesMatchASerialPass(t *testing.T) {
	useFourProcs(t)
	block := largeBlock(rand.New(rand.NewSource(2)), 20000)
	sortMutationsByKey(block)
	budlets := make([]*Budlet, len(block))
	want := make([][32]byte, len(block))
	for i := range block {
		budlet, err := budletFromMutation(&block[i])
		require.NoError(t, err)
		budlets[i] = budlet
		want[i], _ = budLeafHash(budlet, nil)
	}
	require.Equal(t, want, budLeafHashes(budlets))
}

func TestBUDHashFromMutationsReportsTheFirstInvalidMutation(t *testing.T) {
	useFourProcs(t)
	block := largeBlock(rand.New(rand.NewSource(3)), 20000)
	sortMutationsByKey(block)
	shortValue := vtype.SerializeMisc(4, nil)[:vtype.VersionLength+vtype.BlockHeightLength-1]
	block[15000].NewValue = shortValue
	block[9000].NewValue = shortValue
	_, err := budHashFromMutations(block)
	require.ErrorContains(t, err, "failed to convert mutation 9000:")
}
