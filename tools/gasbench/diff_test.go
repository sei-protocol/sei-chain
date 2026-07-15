package gasbench

import (
	"testing"
	"time"
)

// TestSubtractPerPass pins the slimmed per-pass contract: Subtract carries the
// point median delta and the exact gas delta, with no significance verdict
// (distinguishability now lives at the cross-run layer, crossrun.go).
func TestSubtractPerPass(t *testing.T) {
	baseline := Series{
		GasUsed: 100,
		Samples: []time.Duration{100 * time.Nanosecond, 102 * time.Nanosecond, 104 * time.Nanosecond},
	}
	target := Series{
		GasUsed: 130,
		Samples: []time.Duration{110 * time.Nanosecond, 112 * time.Nanosecond, 114 * time.Nanosecond},
	}

	d := Subtract("ADD", baseline, target, 10, 0.25)

	if d.DeltaNs != 10 {
		t.Errorf("DeltaNs = %v, want 10 (target median - baseline median)", d.DeltaNs)
	}
	if d.GasDelta != 30 {
		t.Errorf("GasDelta = %d, want 30", d.GasDelta)
	}
	if d.PerOpNs != 1 {
		t.Errorf("PerOpNs = %v, want 1 (DeltaNs/reps)", d.PerOpNs)
	}
}
