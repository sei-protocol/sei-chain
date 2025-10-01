package reservoir

import (
	"sync"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestNewPanicsOnNonPositiveSize(t *testing.T) {
	t.Parallel()
	for _, invalidSize := range []int{-3, -1, 0} {
		require.Panicsf(t, func() { New[int](invalidSize, 0.1, nil) }, "New(%d, nil) did not panic", invalidSize)
	}
}

func TestAddAndSeenBehavior(t *testing.T) {
	t.Parallel()
	const (
		k = 16
		n = 1000
	)

	subject := New[int](k, 0.1, nil)

	// Add fewer than k: reservoir grows to match added items.
	for i := 0; i < k-3; i++ {
		subject.Add(i)
	}
	require.Equalf(t, k-3, len(subject.samples), "len(samples) (before filling)")
	require.EqualValuesf(t, k-3, subject.Seen(), "Seen() (before filling)")

	// Add up to and beyond capacity.
	for i := k - 3; i < n; i++ {
		subject.Add(i)
	}

	// Seen() tracks all inserted elements.
	require.EqualValues(t, n, subject.Seen())
	// Reservoir never exceeds k.
	require.Equal(t, k, len(subject.samples))

	// Sanity check: all sample values are from the input domain.
	for _, v := range subject.samples {
		require.Truef(t, v >= 0 && v < n, "sample out of range: %d (expected [0,%d))", v, n)
	}
}

func TestPercentileEmpty(t *testing.T) {
	t.Parallel()
	s := New[int](8, 0.5, nil)
	// nothing added; samples is empty
	percentile, ok := s.Percentile()
	require.False(t, ok, "Percentile on empty reservoir should return ok=false")
	require.Zero(t, percentile, "Percentile on empty reservoir should return zero value")
}

func TestPercentileNearestRankAndClamping(t *testing.T) {
	t.Parallel()

	for _, test := range []struct {
		name       string
		percentile float64
		want       int
	}{
		{
			name:       "clamp_low",
			percentile: -1.0,
			want:       1,
		},
		{
			name:       "zero_is_min",
			percentile: 0.0,
			want:       1,
		},
		{
			name:       "very_low",
			percentile: 0.01,
			want:       1,
		},
		{
			name:       "20th",
			percentile: 0.20,
			want:       1,
		},
		{
			name:       "just_above_20th",
			percentile: 0.21,
			want:       2,
		},
		{
			name:       "40th",
			percentile: 0.40,
			want:       2,
		},
		{
			name:       "just_above_40th",
			percentile: 0.41,
			want:       4,
		},
		{
			name:       "60th",
			percentile: 0.60,
			want:       4,
		},
		{
			name:       "just_above_60th",
			percentile: 0.61,
			want:       7,
		},
		{
			name:       "80th",
			percentile: 0.80,
			want:       7,
		},
		{
			name:       "just_above_80th",
			percentile: 0.81,
			want:       9,
		},
		{
			name:       "one_is_max",
			percentile: 1.0,
			want:       9,
		},
		{
			name:       "clamp_high",
			percentile: 2.0,
			want:       9,
		},
	} {
		subject := New[int](5, test.percentile, nil)
		// Overwrite the internal samples directly for deterministic testing, unsorted on purpose.
		subject.samples = []int{7, 1, 4, 9, 2}
		got, ok := subject.Percentile()
		require.True(t, ok, "%s: not OK", test.name)
		require.Equalf(t, test.want, got, "%s: Percentile(%v) value mismatch", test.name, test.percentile)
	}
}

func TestConcurrentAddsAreThreadSafe(t *testing.T) {
	t.Parallel()
	const (
		k          = 64
		goroutines = 8
		perG       = 10_000
		total      = goroutines * perG
	)

	subject := New[int](k, 0.5, nil)

	var wg sync.WaitGroup
	wg.Add(goroutines)
	for g := 0; g < goroutines; g++ {
		go func(offset int) {
			defer wg.Done()
			start := offset * perG
			for i := 0; i < perG; i++ {
				subject.Add(start + i)
			}
		}(g)
	}
	wg.Wait()

	require.EqualValues(t, total, subject.Seen())
	require.EqualValues(t, k, len(subject.samples))

	// Make sure Percentile does not panic while others might be adding.
	// (No concurrent adds here, just exercise the lock path.)
	_, ok := subject.Percentile()
	require.True(t, ok, "Percentile should succeed on non-empty reservoir")
}

func TestReservoirCoversRangeLoosely(t *testing.T) {
	t.Parallel()
	const (
		k = 128
		n = 50_000
	)

	subject := New[int](k, 0.0, nil)
	for i := range n {
		subject.Add(i)
	}
	// Expect min sample not too close to n and max not too close to 0.
	// With uniform sampling, these should typically be well inside the range.
	lowest, ok := subject.Percentile()
	require.True(t, ok)

	subject = New[int](k, 1.0, nil)
	for i := range n {
		subject.Add(i)
	}

	highest, ok := subject.Percentile()
	require.True(t, ok)

	require.True(t, lowest >= 0 && highest < n, "min/max out of domain: min=%d max=%d n=%d", lowest, highest, n)
	require.Lessf(t, lowest, n/2, "min unexpectedly large: min=%d n/2=%d (sampler may be biased)", lowest, n/2)
	require.Greaterf(t, highest, n/2, "max unexpectedly small: max=%d n/2=%d (sampler may be biased)", highest, n/2)
}
