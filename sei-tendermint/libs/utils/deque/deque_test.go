package deque

import (
	"fmt"
	"slices"
	"testing"

	"github.com/sei-protocol/sei-chain/sei-tendermint/libs/utils/require"
	"github.com/sei-protocol/sei-chain/sei-tendermint/libs/utils/scope"
)

func TestDequeSnapshots(t *testing.T) {
	var q Deque[int]
	empty := q.Snapshot()
	for _, value := range []int{1, 2, 3} {
		q.PushBack(value)
	}
	first := q.Snapshot()
	require.Equal(t, 1, q.PopFront())
	second := q.Snapshot()
	q.PushBack(4)
	require.Equal(t, 0, empty.Len())
	require.Equal(t, []int{1, 2, 3}, slices.Collect(first.All()))
	require.Equal(t, []int{2, 3}, slices.Collect(second.All()))
	require.Equal(t, []int{2, 3, 4}, slices.Collect(q.Snapshot().All()))
}

func TestDequePopFrontEmpty(t *testing.T) {
	var q Deque[int]
	require.Panics(t, func() { q.PopFront() })
	q.PushBack(7)
	value := q.PopFront()
	require.Equal(t, 7, value)
	require.Panics(t, func() { q.PopFront() })
}

func TestImmutableDequeSnapshotSurvivesCompaction(t *testing.T) {
	var q Deque[int]
	for i := range 1024 {
		q.PushBack(i)
	}
	snapshot := q.Snapshot()
	checkSnapshot := func() error {
		for i := range snapshot.Len() {
			if got := snapshot.At(i); got != i {
				return fmt.Errorf("snapshot[%v] = %v, want %v", i, got, i)
			}
		}
		return nil
	}
	// Read the snapshot concurrently with mutations to detect races under go test -race.
	require.NoError(t, scope.Parallel(func(s scope.ParallelScope) error {
		s.Spawn(func() error {
			for range 1000 {
				if err := checkSnapshot(); err != nil {
					return err
				}
			}
			return nil
		})
		for i := range 768 {
			got := q.PopFront()
			if got != i {
				return fmt.Errorf("PopFront() = %v, want %v", got, i)
			}
		}
		for i := range 1024 {
			q.PushBack(1024 + i)
		}
		return nil
	}))
	require.Equal(t, 1280, q.Len())
	for i := range q.Len() {
		require.Equal(t, 768+i, q.At(i))
	}
	for i := range q.Len() {
		got := q.PopFront()
		require.Equal(t, 768+i, got)
	}
	require.Equal(t, 0, q.Len())
	q.PushBack(-1)
	require.Equal(t, -1, q.At(0))
	got := q.PopFront()
	require.Equal(t, -1, got)
	require.NoError(t, checkSnapshot())
}

func TestImmutableDequeReleasesPrunedStorage(t *testing.T) {
	var q Deque[int]
	for i := range 65536 {
		q.PushBack(i)
	}
	for i := range 65535 {
		got := q.PopFront()
		require.Equal(t, i, got)
	}
	require.Equal(t, 1, q.Len())
	// Capacity includes the discarded prefix and unused tail.
	require.True(t, cap(q.data) <= 512)
	got := q.PopFront()
	require.Equal(t, 65535, got)
	require.Equal(t, 0, cap(q.data))
}

var dequeBenchmarkSink [][32]byte

func BenchmarkImmutableDeque(b *testing.B) {
	for _, size := range []int{16, 1024, 16384} {
		b.Run(fmt.Sprintf("Grow/%d/Deque", size), func(b *testing.B) {
			b.ReportAllocs()
			for b.Loop() {
				var q Deque[[32]byte]
				for range size {
					q.PushBack([32]byte{})
				}
				dequeBenchmarkSink = q.data
			}
		})
		b.Run(fmt.Sprintf("Grow/%d/Slice", size), func(b *testing.B) {
			b.ReportAllocs()
			for b.Loop() {
				var data [][32]byte
				for range size {
					data = append(data, [32]byte{})
				}
				dequeBenchmarkSink = data
			}
		})
		b.Run(fmt.Sprintf("Steady/%d/Deque", size), func(b *testing.B) {
			var q Deque[[32]byte]
			for range size {
				q.PushBack([32]byte{})
			}
			b.ReportAllocs()
			for b.Loop() {
				for range 1024 {
					q.PopFront()
					q.PushBack([32]byte{})
				}
			}
			dequeBenchmarkSink = q.data
		})
		b.Run(fmt.Sprintf("Steady/%d/Slice", size), func(b *testing.B) {
			data := make([][32]byte, size)
			b.ReportAllocs()
			for b.Loop() {
				for range 1024 {
					data = data[1:]
					data = append(data, [32]byte{})
				}
			}
			dequeBenchmarkSink = data
		})
	}
	b.Run("Drain/16384/Deque", func(b *testing.B) {
		var retained int
		b.ReportAllocs()
		for b.Loop() {
			var q Deque[[32]byte]
			for range 16384 {
				q.PushBack([32]byte{})
			}
			for range 16383 {
				q.PopFront()
			}
			retained = cap(q.data)
			dequeBenchmarkSink = q.data
		}
		b.ReportMetric(float64(retained*32), "retained-B")
	})
	b.Run("Drain/16384/Slice", func(b *testing.B) {
		var retained int
		b.ReportAllocs()
		for b.Loop() {
			var data [][32]byte
			for range 16384 {
				data = append(data, [32]byte{})
			}
			retained = cap(data)
			for range 16383 {
				data = data[1:]
			}
			dequeBenchmarkSink = data
		}
		b.ReportMetric(float64(retained*32), "retained-B")
	})
}
