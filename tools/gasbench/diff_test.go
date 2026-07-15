package gasbench

import (
	"testing"
	"time"
)

// TestSubtractNeverSignificantWithZeroUncertainty pins that a series with an
// unestimated variance (fewer than 2 samples, or an improbable zero-variance
// sample) never yields Significant=true, however large the delta -- a raw
// |DeltaNs| > 0 must not stand in for a real effect-size check against an
// uncertainty that was never estimated.
func TestSubtractNeverSignificantWithZeroUncertainty(t *testing.T) {
	low1 := Series{Samples: []time.Duration{100 * time.Nanosecond}}
	high1 := Series{Samples: []time.Duration{100000 * time.Nanosecond}}
	low2 := Series{Samples: []time.Duration{100 * time.Nanosecond, 100 * time.Nanosecond}}
	high2 := Series{Samples: []time.Duration{100000 * time.Nanosecond, 100000 * time.Nanosecond}}

	cases := []struct {
		name     string
		baseline Series
		target   Series
	}{
		{"single-sample baseline, zero-variance target", low1, high2},
		{"zero-variance baseline, single-sample target", low2, high1},
		{"both single-sample", low1, high1},
		{"zero-variance both", low2, high2},
	}

	for _, tc := range cases {
		d := Subtract("ADD", tc.baseline, tc.target, 1, 3, 0.25)
		if d.Uncertainty != 0 {
			t.Errorf("%s: Uncertainty = %v, want 0 for this fixture", tc.name, d.Uncertainty)
			continue
		}
		if d.Significant {
			t.Errorf("%s: Significant = true with Uncertainty == 0 and DeltaNs = %v -- an unestimated uncertainty must never pass the gate",
				tc.name, d.DeltaNs)
		}
	}
}
