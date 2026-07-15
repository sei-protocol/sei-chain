package gasbench

import (
	"math"
	"sort"
	"time"
)

// Stats summarizes a sample series. All time fields are nanoseconds.
type Stats struct {
	N      int
	Min    float64 // least-perturbed estimator for CPU-bound work; noise only adds time
	Max    float64
	Mean   float64
	Median float64
	P99    float64
	Stddev float64 // sample stddev (n-1)
	CoV    float64 // Stddev/Mean; advisory only -- see README.md "Acceptance gate"
}

// Summarize computes the full stat set over the samples.
func Summarize(samples []time.Duration) Stats {
	n := len(samples)
	st := Stats{N: n}
	if n == 0 {
		return st
	}
	xs := make([]float64, n)
	var sum float64
	for i, d := range samples {
		xs[i] = float64(d.Nanoseconds())
		sum += xs[i]
	}
	sort.Float64s(xs)

	st.Min = xs[0]
	st.Max = xs[n-1]
	st.Mean = sum / float64(n)
	st.Median = percentile(xs, 50)
	st.P99 = percentile(xs, 99)

	if n > 1 {
		var ss float64
		for _, x := range xs {
			dx := x - st.Mean
			ss += dx * dx
		}
		st.Stddev = math.Sqrt(ss / float64(n-1))
	}
	if st.Mean > 0 {
		st.CoV = st.Stddev / st.Mean
	}
	return st
}

// percentile interpolates linearly between closest ranks (numpy default).
// xs must be sorted ascending.
func percentile(xs []float64, p float64) float64 {
	n := len(xs)
	if n == 1 {
		return xs[0]
	}
	rank := p / 100 * float64(n-1)
	lo := int(math.Floor(rank))
	hi := int(math.Ceil(rank))
	if lo == hi {
		return xs[lo]
	}
	return xs[lo] + (rank-float64(lo))*(xs[hi]-xs[lo])
}
