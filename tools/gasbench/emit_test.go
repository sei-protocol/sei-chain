package gasbench

import "testing"

// TestNewRunStatusIsSignificantOnly pins that Status is a pure function of
// Significant: HighVariance (CoV) must never flip it. See README.md
// "Acceptance gate".
func TestNewRunStatusIsSignificantOnly(t *testing.T) {
	c := Case{OpcodeID: "ADD", Class: ClassArithmetic}

	cases := []struct {
		significant  bool
		highVariance bool
		want         string
	}{
		{significant: true, highVariance: false, want: StatusOK},
		{significant: true, highVariance: true, want: StatusOK},
		{significant: false, highVariance: false, want: StatusInsignificant},
		{significant: false, highVariance: true, want: StatusInsignificant},
	}

	for _, tc := range cases {
		d := Diff{Significant: tc.significant, HighVariance: tc.highVariance}
		got := NewRun(c, d, 0).Status
		if got != tc.want {
			t.Errorf("Significant=%v HighVariance=%v: Status = %q, want %q",
				tc.significant, tc.highVariance, got, tc.want)
		}
	}
}
