package gasbench

import (
	"bytes"
	"math"
	"testing"
)

// TestDistinguishableReadsRawBounds pins the gate (F1): it reads the raw CI
// bounds and is ±Inf-safe -- -Inf is not > 0 and +Inf is not < 0, so an
// underpowered (non-finite) CI is never distinguishable.
func TestDistinguishableReadsRawBounds(t *testing.T) {
	cases := []struct {
		name   string
		lo, hi float64
		want   bool
	}{
		{"wholly above zero", 1, 3, true},
		{"wholly below zero", -3, -1, true},
		{"straddles zero", -1, 2, false},
		{"lower bound touches zero", 0, 5, false},
		{"underpowered ±Inf", math.Inf(-1), math.Inf(1), false},
	}
	for _, tc := range cases {
		if got := distinguishable(tc.lo, tc.hi); got != tc.want {
			t.Errorf("%s: distinguishable(%v,%v) = %v, want %v", tc.name, tc.lo, tc.hi, got, tc.want)
		}
	}
}

// TestClassifyStatus pins the verdict matrix: underpowered short-circuits
// first; ok requires BOTH distinguishable and effectPass; distinguishable but
// below the floor is sub_threshold; otherwise insignificant.
func TestClassifyStatus(t *testing.T) {
	cases := []struct {
		name                                      string
		underpowered, distinguishable, effectPass bool
		want                                      string
	}{
		{"underpowered wins over all", true, true, true, StatusUnderpowered},
		{"underpowered wins even if not distinguishable", true, false, false, StatusUnderpowered},
		{"distinguishable + effect => ok", false, true, true, StatusOK},
		{"distinguishable but below floor => sub_threshold", false, true, false, StatusSubThreshold},
		{"not distinguishable => insignificant", false, false, true, StatusInsignificant},
		{"not distinguishable, no effect => insignificant", false, false, false, StatusInsignificant},
	}
	for _, tc := range cases {
		if got := classifyStatus(tc.underpowered, tc.distinguishable, tc.effectPass); got != tc.want {
			t.Errorf("%s: classifyStatus(up=%v,dist=%v,eff=%v) = %q, want %q",
				tc.name, tc.underpowered, tc.distinguishable, tc.effectPass, got, tc.want)
		}
	}
}

// TestEffectSizePass pins the practical-half gate: absolute per-op ns clears the
// floor; a non-positive floor disables it.
func TestEffectSizePass(t *testing.T) {
	cases := []struct {
		name                     string
		perOpDeltaNs, minPerOpNs float64
		want                     bool
	}{
		{"above floor", 5, 1, true},
		{"below floor", 0.5, 1, false},
		{"at floor", 1, 1, true},
		{"negative delta above floor by magnitude", -5, 1, true},
		{"floor disabled (zero)", 0.001, 0, true},
		{"floor disabled (negative)", 0.001, -1, true},
	}
	for _, tc := range cases {
		if got := effectSizePass(tc.perOpDeltaNs, tc.minPerOpNs); got != tc.want {
			t.Errorf("%s: effectSizePass(%v,%v) = %v, want %v", tc.name, tc.perOpDeltaNs, tc.minPerOpNs, got, tc.want)
		}
	}
}

// TestNewRunUnderpoweredNullCIAndNDJSONSafe covers D-1: a non-finite CI maps to
// nil (JSON null / CSV empty) and does not break WriteNDJSON with an
// UnsupportedValueError. The status short-circuits to underpowered.
func TestNewRunUnderpoweredNullCIAndNDJSONSafe(t *testing.T) {
	c := Case{OpcodeID: "ADD", Class: ClassArithmetic, Reps: 1000, ConstGas: 3}
	up := crossRun{MedianDeltaNs: 3, CILo: math.Inf(-1), CIHi: math.Inf(1), P: 1, Underpowered: true}

	r := NewRun(c, up, 100, 1, 20000, hostHealth{}, 1.0)
	if r.CILo != nil || r.CIHi != nil {
		t.Errorf("underpowered CI must map to nil, got Lo=%v Hi=%v", r.CILo, r.CIHi)
	}
	if r.Distinguishable {
		t.Error("underpowered row must not be distinguishable")
	}
	if r.Status != StatusUnderpowered {
		t.Errorf("status = %q, want %q", r.Status, StatusUnderpowered)
	}

	var nd bytes.Buffer
	if err := WriteNDJSON(&nd, []Run{r}); err != nil {
		t.Fatalf("WriteNDJSON of an underpowered row must not error (D-1): %v", err)
	}
	var csvBuf bytes.Buffer
	if err := WriteCSV(&csvBuf, []Run{r}); err != nil {
		t.Fatalf("WriteCSV of an underpowered row must not error: %v", err)
	}
	if !bytes.Contains(csvBuf.Bytes(), []byte(",,")) {
		t.Errorf("underpowered CSV must render empty CI fields, got:\n%s", csvBuf.String())
	}
}

// TestNewRunDistinguishableFiniteCI pins the ok path: a finite CI excluding
// zero AND a per-op delta above the floor yields distinguishable + effect_size_pass
// + non-nil bounds + status ok. Per-op = 30000/1000 = 30ns >> the 1.0ns floor.
func TestNewRunDistinguishableFiniteCI(t *testing.T) {
	c := Case{OpcodeID: "DIV", Class: ClassArithmetic, Reps: 1000, ConstGas: 5}
	fin := crossRun{MedianDeltaNs: 30000, CILo: 28000, CIHi: 32000, Confidence: 0.95, P: 4e-5, Alpha: 0.05}

	r := NewRun(c, fin, 5000, 10, 20000, hostHealth{CoV: 0.01}, 1.0)
	if r.CILo == nil || r.CIHi == nil {
		t.Fatal("finite CI must be non-nil")
	}
	if !r.Distinguishable {
		t.Error("CI excluding zero must be distinguishable")
	}
	if !r.EffectSizePass {
		t.Error("30ns/op must clear the 1.0ns floor")
	}
	if r.Status != StatusOK {
		t.Errorf("status = %q, want %q", r.Status, StatusOK)
	}
}

// TestNewRunEffectFloorDemotes (AC9): a distinguishable row whose per-op delta
// is below the floor is demoted to sub_threshold -- distinguishable, but not
// meaningfully more expensive. Per-op = 500/1000 = 0.5ns < the 1.0ns floor.
func TestNewRunEffectFloorDemotes(t *testing.T) {
	c := Case{OpcodeID: "ADD", Class: ClassArithmetic, Reps: 1000, ConstGas: 3}
	cr := crossRun{MedianDeltaNs: 500, CILo: 400, CIHi: 600, Confidence: 0.95, P: 1e-4, Alpha: 0.05}

	r := NewRun(c, cr, 1000, 10, 20000, hostHealth{}, 1.0)
	if !r.Distinguishable {
		t.Error("CI excluding zero must be distinguishable")
	}
	if r.EffectSizePass {
		t.Error("0.5ns/op must NOT clear the 1.0ns floor")
	}
	if r.Status != StatusSubThreshold {
		t.Errorf("status = %q, want %q", r.Status, StatusSubThreshold)
	}
}

// TestNewRunEffectFloorPasses (AC10): the same shape scaled above the floor is ok.
// Per-op = 5000/1000 = 5ns > the 1.0ns floor.
func TestNewRunEffectFloorPasses(t *testing.T) {
	c := Case{OpcodeID: "ADD", Class: ClassArithmetic, Reps: 1000, ConstGas: 3}
	cr := crossRun{MedianDeltaNs: 5000, CILo: 4000, CIHi: 6000, Confidence: 0.95, P: 1e-4, Alpha: 0.05}

	r := NewRun(c, cr, 1000, 10, 20000, hostHealth{}, 1.0)
	if !r.EffectSizePass {
		t.Error("5ns/op must clear the 1.0ns floor")
	}
	if r.Status != StatusOK {
		t.Errorf("status = %q, want %q", r.Status, StatusOK)
	}
}
