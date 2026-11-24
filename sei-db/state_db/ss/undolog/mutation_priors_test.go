package undolog

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/sei-protocol/sei-chain/sei-db/state_db/giga/types/mutation"
)

func TestMutationPriorsYieldEachKeyWithItsPreviousValue(t *testing.T) {
	priors := mutationPriors{
		{Key: []byte("evm/\x0a-account"), PreviousValue: []byte{1}, NewValue: []byte{2}},
		{Key: []byte("bank/created"), NewValue: []byte{3}},
		{Key: []byte("bank/deleted"), PreviousValue: []byte{4}},
	}
	require.Equal(t, 3, priors.Len())
	var got []mutation.Mutation
	for key, prior := range priors.All() {
		got = append(got, mutation.Mutation{Key: key, PreviousValue: prior})
	}
	require.Equal(t, []mutation.Mutation{
		{Key: []byte("evm/\x0a-account"), PreviousValue: []byte{1}},
		{Key: []byte("bank/created")},
		{Key: []byte("bank/deleted"), PreviousValue: []byte{4}},
	}, got)
}

func TestMutationPriorsStopWhenTheConsumerDoes(t *testing.T) {
	priors := mutationPriors{{Key: []byte("bank/a")}, {Key: []byte("bank/b")}}
	seen := 0
	for range priors.All() {
		seen++
		break
	}
	require.Equal(t, 1, seen)
}
