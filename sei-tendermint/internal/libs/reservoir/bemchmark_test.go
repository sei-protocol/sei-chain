package reservoir

import (
	"fmt"
	"testing"
)

func BenchmarkAddSteadyState(b *testing.B) {
	for _, k := range []int{16, 64, 256, 1024} {
		b.Run(fmt.Sprintf("k=%d", k), func(b *testing.B) {
			s := New[int](k, 0.1, nil)
			// Prefill to capacity so Add does replacement logic.
			for i := 0; i < k; i++ {
				s.Add(i)
			}
			b.ResetTimer()
			for i := 0; i < b.N; i++ {
				s.Add(i + k)
			}
		})
	}
}

// Benchmarks Percentile on different reservoir sizes.
func BenchmarkPercentile(b *testing.B) {
	for _, k := range []int{16, 64, 256, 1024} {
		b.Run(fmt.Sprintf("k=%d", k), func(b *testing.B) {
			s := New[int](k, 0.1, nil)
			for i := 0; i < 10_000; i++ {
				s.Add(i)
			}
			b.ReportAllocs()
			b.ResetTimer()
			var sink int
			for i := 0; i < b.N; i++ {
				v, _ := s.Percentile()
				sink ^= v
			}
			_ = sink
		})
	}
}

// Parallel Add benchmark to exercise the mutex under contention.
func BenchmarkAddParallel(b *testing.B) {
	const k = 256
	s := New[int](k, 0.1, nil)
	for i := 0; i < k; i++ {
		s.Add(i)
	}
	b.ResetTimer()
	b.RunParallel(func(pb *testing.PB) {
		x := 0
		for pb.Next() {
			s.Add(x)
			x++
		}
	})
}
