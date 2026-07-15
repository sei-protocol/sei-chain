package gasbench

import (
	"math"
	"slices"

	"golang.org/x/perf/benchmath"
)

// crossRunInput is one opcode's cross-run series: one entry per -count pass.
// Named fields prevent transposing the three same-typed slices at the call
// site. Deltas[k] = TargetMedians[k] - BaselineMedians[k].
type crossRunInput struct {
	BaselineMedians []float64 // within-run median per count, ns
	TargetMedians   []float64 // within-run median per count, ns
	Deltas          []float64 // per-count delta, ns
}

// crossRun is the benchmath verdict for one opcode's cross-run series.
//
// CILo/CIHi are the raw order-statistic CI bounds on the delta and may be
// non-finite (too few counts). The acceptance gate reads them raw (a ±Inf
// bound is simply not > 0 / < 0, so an underpowered row never reads
// distinguishable); the emit layer maps non-finite to a null column.
type crossRun struct {
	MedianDeltaNs float64 // Summary.Center -- median whole-program delta
	CILo, CIHi    float64 // Summary.Lo/Hi -- order-statistic CI; ±Inf when underpowered
	Confidence    float64 // Summary.Confidence -- actual (>= requested)
	P             float64 // Compare.P -- advisory only, never the gate
	Alpha         float64 // Compare.Alpha
	Underpowered  bool    // < 2 counts, or a non-finite CI bound
	Warnings      []error // Summary.Warnings ++ Compare.Warnings
}

// analyzeCrossRun runs the paired delta CI (the acceptance gate, via Summary
// over the per-count deltas) and the advisory unpaired Mann-Whitney p (via
// Compare over the baseline vs target median series). The paired CI is the
// primitive that survives common-run drift; the p-value is surfaced for
// cross-checking only. Inputs are cloned: NewSample sorts in place.
func analyzeCrossRun(in crossRunInput, alpha, confidence float64) crossRun {
	thr := &benchmath.Thresholds{CompareAlpha: alpha}

	sum := benchmath.AssumeNothing.Summary(benchmath.NewSample(slices.Clone(in.Deltas), thr), confidence)
	cmp := benchmath.AssumeNothing.Compare(
		benchmath.NewSample(slices.Clone(in.BaselineMedians), thr),
		benchmath.NewSample(slices.Clone(in.TargetMedians), thr),
	)

	return crossRun{
		MedianDeltaNs: sum.Center,
		CILo:          sum.Lo,
		CIHi:          sum.Hi,
		Confidence:    sum.Confidence,
		P:             cmp.P,
		Alpha:         cmp.Alpha,
		Underpowered:  len(in.Deltas) < 2 || math.IsInf(sum.Lo, 0) || math.IsInf(sum.Hi, 0),
		Warnings:      slices.Concat(sum.Warnings, cmp.Warnings),
	}
}
