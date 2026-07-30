package gasbench

import (
	"math"
	"testing"
)

// TestAnalyzeCrossRunPairedSurvivesDrift is the F1 scenario: a true +3ns/op shift
// under common-mode drift makes the baseline/target median series overlap, so the
// advisory unpaired p is WEAK (fails to distinguish), yet the paired delta CI
// cleanly excludes zero. This is the reason the gate reads the paired CI, not
// Compare -- so the test asserts both sides of that split.
func TestAnalyzeCrossRunPairedSurvivesDrift(t *testing.T) {
	base := []float64{200, 215, 208, 230, 205, 220, 212, 218, 209, 225}
	tgt := make([]float64, len(base))
	deltas := make([]float64, len(base))
	for i, b := range base {
		tgt[i] = b + 3
		deltas[i] = tgt[i] - b
	}
	cr := analyzeCrossRun(crossRunInput{BaselineMedians: base, TargetMedians: tgt, Deltas: deltas}, 0.05, 0.95)

	if cr.Underpowered {
		t.Fatalf("n=10 must not be underpowered")
	}
	if math.IsInf(cr.CILo, 0) || math.IsInf(cr.CIHi, 0) {
		t.Fatalf("n=10 CI must be finite, got [%v, %v]", cr.CILo, cr.CIHi)
	}
	// The advisory unpaired test must FAIL to distinguish under drift -- the whole
	// point of gating on the paired CI instead.
	if cr.P <= cr.Alpha {
		t.Errorf("unpaired p should be weak under drift (> alpha), got P=%v alpha=%v", cr.P, cr.Alpha)
	}
	// The paired CI must exclude zero.
	if cr.CILo <= 0 {
		t.Errorf("paired CI must exclude zero (Lo>0), got Lo=%v", cr.CILo)
	}
	if cr.MedianDeltaNs != 3 {
		t.Errorf("median delta got %v, want 3", cr.MedianDeltaNs)
	}
}

// TestAnalyzeCrossRunStraddlesZero pins the finite-CI-includes-zero path: a
// zero-centered delta series is measurable (not underpowered) but the CI
// straddles zero, so the gate must NOT read it as distinguishable.
func TestAnalyzeCrossRunStraddlesZero(t *testing.T) {
	deltas := []float64{-4, -3, -1, 0, 1, 1, 2, 3, 4, 5}
	base := make([]float64, len(deltas))
	tgt := make([]float64, len(deltas))
	for i, d := range deltas {
		base[i] = 100
		tgt[i] = 100 + d
	}
	cr := analyzeCrossRun(crossRunInput{BaselineMedians: base, TargetMedians: tgt, Deltas: deltas}, 0.05, 0.95)

	if cr.Underpowered {
		t.Fatalf("n=10 must not be underpowered")
	}
	if math.IsInf(cr.CILo, 0) || math.IsInf(cr.CIHi, 0) {
		t.Fatalf("n=10 CI must be finite, got [%v, %v]", cr.CILo, cr.CIHi)
	}
	if !(cr.CILo <= 0 && cr.CIHi >= 0) {
		t.Errorf("zero-centered deltas: CI must straddle zero, got [%v, %v]", cr.CILo, cr.CIHi)
	}
}

// TestAnalyzeCrossRunUnderpowered pins the degenerate single-count path: a finite
// point median but a non-finite CI, flagged underpowered, no panic.
func TestAnalyzeCrossRunUnderpowered(t *testing.T) {
	cr := analyzeCrossRun(crossRunInput{
		BaselineMedians: []float64{200},
		TargetMedians:   []float64{203},
		Deltas:          []float64{3},
	}, 0.05, 0.95)

	if !cr.Underpowered {
		t.Fatalf("n=1 must be underpowered")
	}
	if cr.MedianDeltaNs != 3 {
		t.Errorf("point median of a single delta got %v, want 3", cr.MedianDeltaNs)
	}
	if !math.IsInf(cr.CILo, -1) || !math.IsInf(cr.CIHi, 1) {
		t.Errorf("n=1 CI bounds got [%v, %v], want ±Inf", cr.CILo, cr.CIHi)
	}
	if len(cr.Warnings) == 0 {
		t.Error("n=1 must surface at least one benchmath warning")
	}
}
