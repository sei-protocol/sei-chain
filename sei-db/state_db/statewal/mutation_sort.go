package statewal

import (
	"bytes"
	"runtime"
	"slices"
	"sync"

	"github.com/sei-protocol/sei-chain/sei-db/state_db/giga/types/mutation"
)

// parallelMutationThreshold is the fewest mutations a step splits across goroutines; below it one goroutine
// is faster.
const parallelMutationThreshold = 4096

// maxMutationWorkers bounds the goroutines one step splits a block's mutations across.
const maxMutationWorkers = 16

func compareMutationKeys(a mutation.Mutation, b mutation.Mutation) int {
	return bytes.Compare(a.Key, b.Key)
}

// mutationWorkers returns how many goroutines a step over n mutations uses.
func mutationWorkers(n int) int {
	if n < parallelMutationThreshold {
		return 1
	}
	return max(1, min(maxMutationWorkers, runtime.GOMAXPROCS(0), n/(parallelMutationThreshold/4)))
}

// forEachMutationChunk calls visit over [0, n) split into contiguous chunks, on several goroutines when n is
// large, and returns once every chunk is done.
func forEachMutationChunk(n int, visit func(start int, end int)) {
	workers := mutationWorkers(n)
	if workers == 1 {
		visit(0, n)
		return
	}
	chunk := (n + workers - 1) / workers
	var wg sync.WaitGroup
	for start := 0; start < n; start += chunk {
		wg.Go(func() { visit(start, min(start+chunk, n)) })
	}
	wg.Wait()
}

// sortMutationsByKey sorts a block's mutations by key, which are unique within a block. A large block is
// sorted as runs on separate goroutines, which are then merged pairwise.
func sortMutationsByKey(mutations []mutation.Mutation) {
	n := len(mutations)
	workers := mutationWorkers(n)
	if workers == 1 {
		slices.SortFunc(mutations, compareMutationKeys)
		return
	}
	width := (n + workers - 1) / workers
	forEachRun(n, width, func(start int, end int) {
		slices.SortFunc(mutations[start:end], compareMutationKeys)
	})
	src, dst := mutations, make([]mutation.Mutation, n)
	for ; width < n; width *= 2 {
		forEachRun(n, 2*width, func(start int, end int) {
			mid := min(start+width, end)
			mergeMutationRuns(dst[start:end], src[start:mid], src[mid:end])
		})
		src, dst = dst, src
	}
	if &src[0] != &mutations[0] {
		copy(mutations, src)
	}
}

// forEachRun calls visit for each run of width over [0, n), each on its own goroutine, and returns once every
// run is done.
func forEachRun(n int, width int, visit func(start int, end int)) {
	var wg sync.WaitGroup
	for start := 0; start < n; start += width {
		wg.Go(func() { visit(start, min(start+width, n)) })
	}
	wg.Wait()
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
