package gasbench

import "math"

// Diff is the subtraction of a baseline series (opcode replaced by a
// JUMPDEST/no-op) from a target series (opcode present). Everything the two
// programs share -- loop overhead, setup, timer cost -- cancels in the mean;
// what remains is attributable to the opcode.
//
// The estimator here is difference-of-medians with the median's asymptotic
// standard error propagated in quadrature. Medians reject the additive-noise
// tail better than means. A min-difference with a bootstrap CI is the
// estimator to reach for if the central assumption proves too coarse; it is
// the honest follow-up, not the MVP.
//
// Acceptance is Significant, not CoV. A raw per-series CoV of several percent
// under plain core-pinning (no kernel-level isolcpus/nohz_full/rcu_nocbs) is
// expected physics -- the periodic scheduler tick, device IRQs, and
// shared-cache contention that pinning alone cannot remove -- not a defect.
// No major benchmarking harness (JMH, criterion.rs, Go's own benchstat) gates
// on a fixed dispersion threshold either; all three gate on effect size
// against the estimate's own uncertainty, which is what Significant computes.
// See designs/gas-repricing-telemetry/research/microbenchmark-noise-isolation-tradeoffs.md.
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

	// HighVariance is an advisory-only flag: true when either series' CoV
	// exceeds CoVCeiling. It does not gate Status; it exists to catch a
	// genuinely pathological run (a noisy neighbor, a throttling event) --
	// distinct from the routine several-percent CoV a pinned-but-not-isolated
	// core sees, which HighVariance's default ceiling is set well above.
	HighVariance bool
	CoVCeiling   float64

	// BaselineNivcsw/TargetNivcsw are involuntary-context-switch counts
	// observed during each series' timed window (process-wide; see
	// Series.NivcswDelta). Nonzero values are direct evidence the kernel
	// preempted the measurement thread mid-window -- the mechanism behind
	// HighVariance and behind ordinary CoV, made observable rather than
	// merely inferred.
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
		BaselineNivcsw: baseline.NivcswDelta,
		TargetNivcsw:   target.NivcswDelta,
		Reps:           reps,
	}
	d.DeltaNs = ts.Median - bs.Median
	d.Uncertainty = math.Hypot(ts.SEMedian, bs.SEMedian)
	d.Significant = math.Abs(d.DeltaNs) > sigmaK*d.Uncertainty
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
