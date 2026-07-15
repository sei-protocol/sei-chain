package gasbench

import (
	"os"
	"strconv"
	"testing"
)

// BenchmarkOpcodes drives one differential measurement per scalar opcode
// Case. See README.md "Running it" for the required -benchtime=1x flag and
// what -count=K actually gives you (not K independent process forks).
func BenchmarkOpcodes(b *testing.B) {
	cases := BuildCases()
	def := DefaultConfig()
	cfg := Config{
		Warmup:     envInt(b, "GASBENCH_WARMUP", def.Warmup),
		Iterations: envInt(b, "GASBENCH_ITERS", def.Iterations),
		DisableGC:  def.DisableGC,
		LockThread: def.LockThread,
	}
	// 0.25 default: see README.md "Acceptance gate" for the calibration.
	covCeiling := envFloat(b, "GASBENCH_COV_CEILING", 0.25)
	alpha := envFloat(b, "GASBENCH_ALPHA", 0.05)
	confidence := envFloat(b, "GASBENCH_CI_CONFIDENCE", 0.95)
	// Effect-size floor: a per-op median delta below this (ns) is distinguishable
	// but not "meaningfully more expensive" -> sub_threshold. See README "Acceptance gate".
	minPerOpNs := envFloat(b, "GASBENCH_MIN_PEROP_NS", 1.0)

	// One accumulator per case, indexed by the case-loop index. Go 1.22+
	// per-iteration loop-var capture makes &collectors[i] inside the closure
	// bind to cases[i]'s slot regardless of how -count reruns interleave.
	collectors := make([]opcodeAccum, len(cases))
	for i, c := range cases {
		b.Run(c.OpcodeID, func(b *testing.B) {
			baseProg, err := NewProgram(c.Baseline)
			if err != nil {
				b.Fatalf("%s: baseline program invalid: %v", c.OpcodeID, err)
			}
			tgtProg, err := NewProgram(c.Target)
			if err != nil {
				b.Fatalf("%s: target program invalid: %v", c.OpcodeID, err)
			}

			base, err := Measure(c.OpcodeID+"/baseline", baseProg.Run, cfg)
			if err != nil {
				b.Fatal(err)
			}
			tgt, err := Measure(c.OpcodeID+"/target", tgtProg.Run, cfg)
			if err != nil {
				b.Fatal(err)
			}

			d := Subtract(c.OpcodeID, base, tgt, c.Reps, covCeiling)

			// Self-check of the harness construction (see README.md "Output
			// schema"), not of opcode timing. Insensitive to a
			// mis-transcribed OpSpec.Arity that still yields a
			// self-consistent ExpectedGasDelta -- see AGENTS.md.
			// Lock-free like sink (gasbench.go): subtests and -count reruns are
			// sequential (no b.Parallel). A future parallelize needs a mutex.
			acc := &collectors[i]
			acc.iterations = cfg.Iterations
			if d.GasDelta != c.ExpectedGasDelta {
				b.Errorf("%s: gas self-check failed: measured delta %d != expected %d (baseline/target pair does not isolate the opcode)",
					c.OpcodeID, d.GasDelta, c.ExpectedGasDelta)
				// b.Errorf does not unwind; taint the case so aggregate emits
				// status=error instead of a normal-looking row built on a
				// construction that failed its own invariant.
				acc.selfCheckFailed = true
			}
			// The gas delta is definitional and must be identical every count;
			// a drift means the differential construction is not deterministic.
			if acc.gasDeltaSet && d.GasDelta != acc.gasDelta {
				b.Errorf("%s: gas delta not identical across counts: %d != first-count %d",
					c.OpcodeID, d.GasDelta, acc.gasDelta)
				acc.selfCheckFailed = true
			}
			if !acc.gasDeltaSet {
				acc.gasDelta, acc.gasDeltaSet = d.GasDelta, true
			}
			acc.baseMedians = append(acc.baseMedians, d.BaselineMedian)
			acc.tgtMedians = append(acc.tgtMedians, d.TargetMedian)
			acc.deltas = append(acc.deltas, d.DeltaNs)
			acc.covMax = max(acc.covMax, d.BaselineCoV, d.TargetCoV)
			acc.highVariance = acc.highVariance || d.HighVariance
			acc.nvcsw = max(acc.nvcsw, d.BaselineNvcsw, d.TargetNvcsw)
			acc.nivcsw = max(acc.nivcsw, d.BaselineNivcsw, d.TargetNivcsw)

			b.ReportMetric(d.BaselineMedian, "baseline-median-ns")
			b.ReportMetric(d.TargetMedian, "target-median-ns")
			b.ReportMetric(d.PerOpNs, "per-op-ns")
			// per-op-gas-delta is differential (net of filler); const-gas is the
			// nominal denominator for ns-per-gas.
			b.ReportMetric(d.PerOpGas, "per-op-gas-delta")
			b.ReportMetric(float64(c.ConstGas), "const-gas")

			for _, w := range append(base.Warnings, tgt.Warnings...) {
				b.Logf("%s: %s", c.OpcodeID, w)
			}
			if d.HighVariance {
				b.Logf("%s: series CoV above health-check ceiling %.4g (baseline=%.4g target=%.4g, nivcsw base=%d tgt=%d) -- advisory only, does not gate the verdict",
					c.OpcodeID, covCeiling, d.BaselineCoV, d.TargetCoV, base.NivcswDelta, tgt.NivcswDelta)
			}
		})
	}

	// Aggregate once, in Specs order, after all counts have accumulated.
	runs := make([]Run, 0, len(cases))
	for i := range cases {
		runs = append(runs, aggregate(b, &collectors[i], cases[i], alpha, confidence, minPerOpNs))
	}
	writeRuns(b, runs)
}

// opcodeAccum accumulates one opcode's per-count raw data across the -count
// reruns of its subtest. Test-local: production analyzeCrossRun takes
// crossRunInput instead. The Case is not stored here -- aggregation reads it
// from cases[i].
type opcodeAccum struct {
	iterations      int
	baseMedians     []float64 // within-run median per count, ns
	tgtMedians      []float64 // within-run median per count, ns
	deltas          []float64 // per-count delta, ns (tgtMed - baseMed)
	gasDelta        uint64    // exact; guarded identical every count
	gasDeltaSet     bool
	selfCheckFailed bool    // a gas self-check b.Errorf fired; the case is tainted
	covMax          float64 // running max of per-count worse-of-pair CoV (advisory)
	highVariance    bool    // OR across counts (advisory)
	nvcsw           int64   // running max of worse-of-pair voluntary ctx switches
	nivcsw          int64   // running max of worse-of-pair involuntary ctx switches
}

// aggregate builds the cross-run input from the collector, runs the benchmath
// analysis, and composes the per-opcode Run. Thin glue: the verdict lives in
// NewRun/classifyStatus (emit.go).
func aggregate(b *testing.B, acc *opcodeAccum, c Case, alpha, confidence, minPerOpNs float64) Run {
	// A subtest that b.Fatalf'd (invalid program, Measure failure) unwinds only
	// its own goroutine; the parent still aggregates. An empty accumulator
	// through benchmath yields a NaN center that would corrupt the writers --
	// emit the error row instead.
	if len(acc.deltas) == 0 || acc.selfCheckFailed {
		reason := "no measurements accumulated (subtest failed)"
		if acc.selfCheckFailed {
			reason = "gas self-check failed (construction invalid)"
		}
		b.Logf("%s: %s; emitting status=error row", c.OpcodeID, reason)
		return NewErrorRun(c, acc.iterations)
	}
	cr := analyzeCrossRun(crossRunInput{
		BaselineMedians: acc.baseMedians,
		TargetMedians:   acc.tgtMedians,
		Deltas:          acc.deltas,
	}, alpha, confidence)
	// Surface benchmath's cross-run warnings (e.g. too few counts for a finite
	// CI, or n below the U-test's minimum) so an underpowered/degenerate run is
	// visible, not silent.
	for _, w := range cr.Warnings {
		b.Logf("%s: %v", c.OpcodeID, w)
	}
	return NewRun(c, cr, acc.gasDelta, len(acc.deltas), acc.iterations, hostHealth{
		CoV:          acc.covMax,
		HighVariance: acc.highVariance,
		Nvcsw:        acc.nvcsw,
		Nivcsw:       acc.nivcsw,
	}, minPerOpNs)
}

func writeRuns(b *testing.B, runs []Run) {
	if p := os.Getenv("GASBENCH_OUT_CSV"); p != "" {
		writeFile(b, p, "csv", func(f *os.File) error { return WriteCSV(f, runs) })
	}
	if p := os.Getenv("GASBENCH_OUT_NDJSON"); p != "" {
		writeFile(b, p, "ndjson", func(f *os.File) error { return WriteNDJSON(f, runs) })
	}
}

// writeFile checks the Close error too: a failed final flush silently truncates
// the results file the operator then trusts as measurement data.
func writeFile(b *testing.B, path, kind string, write func(*os.File) error) {
	f, err := os.Create(path)
	if err != nil {
		b.Fatalf("create %s: %v", kind, err)
	}
	if err := write(f); err != nil {
		f.Close()
		b.Fatalf("write %s: %v", kind, err)
	}
	if err := f.Close(); err != nil {
		b.Fatalf("close %s: %v", kind, err)
	}
}

// envInt/envFloat fail loud on a set-but-unparseable value rather than silently
// falling back to the default: a typo like GASBENCH_ITERS=2O00 should stop the
// run, not quietly measure something the operator didn't ask for.
func envInt(b *testing.B, key string, def int) int {
	v := os.Getenv(key)
	if v == "" {
		return def
	}
	n, err := strconv.Atoi(v)
	if err != nil {
		b.Fatalf("%s=%q: not an integer: %v", key, v, err)
	}
	return n
}

func envFloat(b *testing.B, key string, def float64) float64 {
	v := os.Getenv(key)
	if v == "" {
		return def
	}
	f, err := strconv.ParseFloat(v, 64)
	if err != nil {
		b.Fatalf("%s=%q: not a float: %v", key, v, err)
	}
	return f
}
