package gasbench

// Diff is the difference-of-medians subtraction of a baseline series from a
// target series for one -count pass. Distinguishability is decided at the
// cross-run layer (crossrun.go), not here: Diff carries only the per-pass
// point medians, delta, gas, and advisory health signals.
type Diff struct {
	OpcodeID string

	BaselineMedian float64 // ns
	TargetMedian   float64 // ns
	BaselineCoV    float64
	TargetCoV      float64

	DeltaNs float64 // per-program time attributable to the opcode reps

	// HighVariance is advisory only (CoV exceeded CoVCeiling). See README.md
	// "Acceptance gate".
	HighVariance bool
	CoVCeiling   float64

	// BaselineNvcsw/TargetNvcsw and BaselineNivcsw/TargetNivcsw are each
	// series' voluntary/involuntary context-switch counts (see
	// Series.NvcswDelta/NivcswDelta). See README.md "Active-benchmarking
	// diagnostics".
	BaselineNvcsw  int64
	TargetNvcsw    int64
	BaselineNivcsw int64
	TargetNivcsw   int64

	Reps    int     // opcode executions per program (from the Case)
	PerOpNs float64 // DeltaNs / Reps

	GasDelta uint64  // measured whole-program gas delta; exact (ground truth)
	PerOpGas float64 // GasDelta / Reps
}

// Subtract computes the per-pass opcode cost from baseline/target series.
// covCeiling is an advisory health-check ceiling, not a gate: a raw per-series
// CoV above it flags the run for a closer look (a possible neighbor or
// throttling event). reps normalizes the per-program delta to one opcode.
func Subtract(opcodeID string, baseline, target Series, reps int, covCeiling float64) Diff {
	bs := Summarize(baseline.Samples)
	ts := Summarize(target.Samples)

	d := Diff{
		OpcodeID:       opcodeID,
		BaselineMedian: bs.Median,
		TargetMedian:   ts.Median,
		BaselineCoV:    bs.CoV,
		TargetCoV:      ts.CoV,
		CoVCeiling:     covCeiling,
		BaselineNvcsw:  baseline.NvcswDelta,
		TargetNvcsw:    target.NvcswDelta,
		BaselineNivcsw: baseline.NivcswDelta,
		TargetNivcsw:   target.NivcswDelta,
		Reps:           reps,
	}
	d.DeltaNs = ts.Median - bs.Median
	d.HighVariance = bs.CoV > covCeiling || ts.CoV > covCeiling

	if target.GasUsed >= baseline.GasUsed {
		d.GasDelta = target.GasUsed - baseline.GasUsed
	}
	if reps > 0 {
		d.PerOpNs = d.DeltaNs / float64(reps)
		d.PerOpGas = float64(d.GasDelta) / float64(reps)
	}
	return d
}
