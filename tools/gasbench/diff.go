package gasbench

import "math"

// Diff is the difference-of-medians subtraction of a baseline series from a
// target series, with the median's standard error propagated in quadrature
// as Uncertainty. See README.md for the estimator choice and the
// Significant-not-CoV acceptance-gate rationale.
type Diff struct {
	OpcodeID string

	BaselineMedian float64 // ns
	TargetMedian   float64 // ns
	BaselineCoV    float64
	TargetCoV      float64

	DeltaNs     float64 // per-program time attributable to the opcode reps
	Uncertainty float64 // 1-sigma, propagated in quadrature (ns)
	SigmaK      float64 // significance multiple applied
	Significant bool    // |DeltaNs| > SigmaK*Uncertainty -- the acceptance gate (see emit.go's Status)

	// HighVariance is advisory only (CoV exceeded CoVCeiling); it never gates
	// Significant. See README.md "Acceptance gate".
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

// Subtract computes the opcode cost from baseline/target series. sigmaK sets
// the significance band (e.g. 3 ~ 99.7% for a normal center) and is the
// acceptance gate. covCeiling is an advisory health-check ceiling, not a
// gate: a raw per-series CoV above it flags the run for a closer look
// (a possible neighbor or throttling event) without invalidating an
// otherwise-significant result. reps normalizes the per-program delta to one
// opcode.
func Subtract(opcodeID string, baseline, target Series, reps int, sigmaK, covCeiling float64) Diff {
	bs := Summarize(baseline.Samples)
	ts := Summarize(target.Samples)

	d := Diff{
		OpcodeID:       opcodeID,
		BaselineMedian: bs.Median,
		TargetMedian:   ts.Median,
		BaselineCoV:    bs.CoV,
		TargetCoV:      ts.CoV,
		SigmaK:         sigmaK,
		CoVCeiling:     covCeiling,
		BaselineNvcsw:  baseline.NvcswDelta,
		TargetNvcsw:    target.NvcswDelta,
		BaselineNivcsw: baseline.NivcswDelta,
		TargetNivcsw:   target.NivcswDelta,
		Reps:           reps,
	}
	d.DeltaNs = ts.Median - bs.Median
	d.Uncertainty = math.Hypot(ts.SEMedian, bs.SEMedian)
	// Uncertainty == 0 (e.g. Iterations < 2, or an improbable zero-variance
	// sample) means no variance was ever estimated, not that the delta is
	// perfectly known -- Significant must never be true from an
	// unestimated Uncertainty.
	d.Significant = d.Uncertainty > 0 && math.Abs(d.DeltaNs) > sigmaK*d.Uncertainty
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
