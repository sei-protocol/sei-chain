package gasbench

import "testing"

// TestNewRunStatusIsSignificantOnly pins that Status is a pure function of
// Significant: neither HighVariance nor the raw CoV values behind it may
// flip it. See README.md "Acceptance gate".
func TestNewRunStatusIsSignificantOnly(t *testing.T) {
	c := Case{OpcodeID: "ADD", Class: ClassArithmetic}

	cases := []struct {
		significant  bool
		highVariance bool
		cov          float64 // fed into BaselineCoV/TargetCoV directly
		want         string
	}{
		{significant: true, highVariance: false, cov: 0, want: StatusOK},
		{significant: true, highVariance: true, cov: 0, want: StatusOK},
		{significant: false, highVariance: false, cov: 0, want: StatusInsignificant},
		{significant: false, highVariance: true, cov: 0, want: StatusInsignificant},
		// A regression that gated Status on CoV directly (rather than on
		// Significant/HighVariance) would flip these two, even though the
		// cases above never exercise a nonzero CoV.
		{significant: true, highVariance: true, cov: 0.9, want: StatusOK},
		{significant: false, highVariance: true, cov: 0.9, want: StatusInsignificant},
	}

	for _, tc := range cases {
		d := Diff{
			Significant:  tc.significant,
			HighVariance: tc.highVariance,
			BaselineCoV:  tc.cov,
			TargetCoV:    tc.cov,
		}
		got := NewRun(c, d, 0).Status
		if got != tc.want {
			t.Errorf("Significant=%v HighVariance=%v CoV=%v: Status = %q, want %q",
				tc.significant, tc.highVariance, tc.cov, got, tc.want)
		}
	}
}
