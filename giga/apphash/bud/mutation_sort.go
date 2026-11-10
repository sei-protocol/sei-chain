package bud

import (
	"bytes"
	"slices"

	"github.com/sei-protocol/sei-chain/giga/apphash/internal/parallel"
	"github.com/sei-protocol/sei-chain/sei-db/state_db/giga/types/mutation"
)

func compareMutationKeys(a mutation.Mutation, b mutation.Mutation) int {
	return bytes.Compare(a.Key, b.Key)
}

// sortMutationsByKey sorts a block's mutations by key in place, as runs sorted in parallel and then
// merged pairwise. Keys are unique within a block, so the result does not depend on how the work is split.
func sortMutationsByKey(mutations []mutation.Mutation) {
	n := len(mutations)
	if n < 2 {
		return
	}
	workers := parallel.Workers(n)
	width := (n + workers - 1) / workers
	parallel.ForEachRun(n, width, func(start int, end int) {
		slices.SortFunc(mutations[start:end], compareMutationKeys)
	})
	src, dst := mutations, make([]mutation.Mutation, n)
	for ; width < n; width *= 2 {
		parallel.ForEachRun(n, 2*width, func(start int, end int) {
			mid := min(start+width, end)
			mergeMutationRuns(dst[start:end], src[start:mid], src[mid:end])
		})
		src, dst = dst, src
	}
	if &src[0] != &mutations[0] {
		copy(mutations, src)
	}
}

// mergeMutationRuns merges two key-sorted runs into dst, which holds exactly their combined length.
func mergeMutationRuns(dst []mutation.Mutation, left []mutation.Mutation, right []mutation.Mutation) {
	i, j := 0, 0
	for k := range dst {
		if j >= len(right) || (i < len(left) && compareMutationKeys(left[i], right[j]) <= 0) {
			dst[k] = left[i]
			i++
		} else {
			dst[k] = right[j]
			j++
		}
	}
}
