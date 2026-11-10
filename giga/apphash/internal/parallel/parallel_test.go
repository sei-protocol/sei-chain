package parallel

import (
	"runtime"
	"slices"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestForEachChunkVisitsEveryIndexOnce(t *testing.T) {
	prev := runtime.GOMAXPROCS(4)
	t.Cleanup(func() { runtime.GOMAXPROCS(prev) })
	for _, n := range []int{0, 1, 3, 4, 5, 10007} {
		visits := make([]int, n)
		ForEachChunk(n, func(start int, end int) {
			for i := start; i < end; i++ {
				visits[i]++
			}
		})
		require.Equal(t, slices.Repeat([]int{1}, n), visits, "n=%d", n)
	}
}

func TestWorkersUsesOneGoroutinePerCPUAndNoMoreThanItems(t *testing.T) {
	prev := runtime.GOMAXPROCS(4)
	t.Cleanup(func() { runtime.GOMAXPROCS(prev) })
	require.Equal(t, 1, Workers(0))
	require.Equal(t, 1, Workers(1))
	require.Equal(t, 3, Workers(3))
	require.Equal(t, 4, Workers(10007))
}
