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
type Diff struct {
	OpcodeID string

	BaselineMedian float64 // ns
	TargetMedian   float64 // ns
	BaselineCoV    float64
	TargetCoV      float64

	DeltaNs     float64 // per-program time attributable to the opcode reps
	Uncertainty float64 // 1-sigma, propagated in quadrature (ns)
	SigmaK      float64 // significance multiple applied
	Significant bool    // |DeltaNs| > SigmaK*Uncertainty

	NoiseOK  bool // both series CoV within the floor
	CoVFloor float64

	Reps    int     // opcode executions per program (from the Case)
	PerOpNs float64 // DeltaNs / Reps

	GasDelta uint64  // measured whole-program gas delta; exact (ground truth)
	PerOpGas float64 // GasDelta / Reps
}

// Subtract computes the opcode cost from baseline/target series. sigmaK sets
// the significance band (e.g. 3 ~ 99.7% for a normal center); covFloor is the
// per-series noise gate; reps normalizes the per-program delta to one opcode.
func Subtract(opcodeID string, baseline, target Series, reps int, sigmaK, covFloor float64) Diff {
	bs := Summarize(baseline.Samples)
	ts := Summarize(target.Samples)

	d := Diff{
		OpcodeID:       opcodeID,
		BaselineMedian: bs.Median,
		TargetMedian:   ts.Median,
		BaselineCoV:    bs.CoV,
		TargetCoV:      ts.CoV,
		SigmaK:         sigmaK,
		CoVFloor:       covFloor,
		Reps:           reps,
	}
	d.DeltaNs = ts.Median - bs.Median
	d.Uncertainty = math.Hypot(ts.SEMedian, bs.SEMedian)
	d.Significant = math.Abs(d.DeltaNs) > sigmaK*d.Uncertainty
	d.NoiseOK = bs.CoV <= covFloor && ts.CoV <= covFloor

	if target.GasUsed >= baseline.GasUsed {
		d.GasDelta = target.GasUsed - baseline.GasUsed
	}
	if reps > 0 {
		d.PerOpNs = d.DeltaNs / float64(reps)
		d.PerOpGas = float64(d.GasDelta) / float64(reps)
	}
	return d
}
