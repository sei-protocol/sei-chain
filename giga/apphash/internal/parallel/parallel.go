// Package parallel splits per-item work of the app hash computations across goroutines.
package parallel

import (
	"runtime"
	"sync"
)

// Workers returns how many goroutines a step over n items uses: one per CPU, and no more than n.
func Workers(n int) int {
	return max(1, min(runtime.GOMAXPROCS(0), n))
}

// ForEachChunk calls visit over [0, n) split into one contiguous chunk per worker, each on its own
// goroutine, and returns once every chunk is done.
func ForEachChunk(n int, visit func(start int, end int)) {
	workers := Workers(n)
	ForEachRun(n, (n+workers-1)/workers, visit)
}

// ForEachRun calls visit for each run of width over [0, n), each on its own goroutine, and returns once
// every run is done.
func ForEachRun(n int, width int, visit func(start int, end int)) {
	var wg sync.WaitGroup
	for start := 0; start < n; start += width {
		wg.Go(func() { visit(start, min(start+width, n)) })
	}
	wg.Wait()
}
