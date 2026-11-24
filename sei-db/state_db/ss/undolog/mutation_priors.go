package undolog

import (
	"iter"

	"github.com/sei-protocol/sei-chain/sei-db/state_db/giga/types/mutation"
)

// mutationPriors reads a block's mutations as the value each key they change held before the block.
type mutationPriors []mutation.Mutation

func (p mutationPriors) Len() int {
	return len(p)
}

func (p mutationPriors) All() iter.Seq2[[]byte, []byte] {
	return func(yield func(key, prior []byte) bool) {
		for i := range p {
			if !yield(p[i].Key, p[i].PreviousValue) {
				return
			}
		}
	}
}
