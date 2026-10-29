package statewal

import (
	"math/rand/v2"
	"slices"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/sei-protocol/sei-chain/sei-db/state_db/giga/types/mutation"
)

// A block large enough to sort on several goroutines must come out exactly as a plain sort leaves it.
func TestSortMutationsByKeyMatchesASerialSort(t *testing.T) {
	rng := rand.New(rand.NewPCG(3, 4))
	for _, n := range []int{0, 1, 100, parallelMutationThreshold - 1, parallelMutationThreshold, 5*parallelMutationThreshold + 13} {
		mutations := make([]mutation.Mutation, n)
		for i := range mutations {
			key := make([]byte, 1+rng.IntN(40))
			for j := range key {
				key[j] = byte(rng.Uint32())
			}
			key = append(key, byte(i>>16), byte(i>>8), byte(i)) // unique within the block
			mutations[i] = mutation.Mutation{Key: key, NewValue: []byte{byte(i)}}
		}
		want := slices.Clone(mutations)
		slices.SortFunc(want, compareMutationKeys)

		sortMutationsByKey(mutations)
		require.Equalf(t, want, mutations, "%d mutations", n)
	}
}
