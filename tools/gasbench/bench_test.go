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
		Warmup:     envInt("GASBENCH_WARMUP", def.Warmup),
		Iterations: envInt("GASBENCH_ITERS", def.Iterations),
		DisableGC:  def.DisableGC,
		LockThread: def.LockThread,
	}
	sigmaK := envFloat("GASBENCH_SIGMA_K", 3)
	// 0.25 default: see README.md "Acceptance gate" for the calibration.
	covCeiling := envFloat("GASBENCH_COV_CEILING", 0.25)

	var runs []Run
	for _, c := range cases {
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

			d := Subtract(c.OpcodeID, base, tgt, c.Reps, sigmaK, covCeiling)

			// Self-check of the harness construction (see README.md "Output
			// schema"), not of opcode timing. Insensitive to a
			// mis-transcribed OpSpec.Arity that still yields a
			// self-consistent ExpectedGasDelta -- see AGENTS.md.
			if d.GasDelta != c.ExpectedGasDelta {
				b.Errorf("%s: gas self-check failed: measured delta %d != expected %d (baseline/target pair does not isolate the opcode)",
					c.OpcodeID, d.GasDelta, c.ExpectedGasDelta)
			}

			b.ReportMetric(d.BaselineMedian, "baseline-median-ns")
			b.ReportMetric(d.TargetMedian, "target-median-ns")
			b.ReportMetric(d.PerOpNs, "per-op-ns")
			b.ReportMetric(d.PerOpGas, "per-op-gas")

			for _, w := range append(base.Warnings, tgt.Warnings...) {
				b.Logf("%s: %s", c.OpcodeID, w)
			}
			// Significant is the acceptance gate; HighVariance is advisory
			// only and never overrides it (see emit.go, diff.go doc comments).
			if !d.Significant {
				b.Logf("%s: delta %.1fns within noise (uncertainty %.1fns, %gσ) -- marginal cost not distinguishable from zero at this precision",
					c.OpcodeID, d.DeltaNs, d.Uncertainty, sigmaK)
			}
			if d.HighVariance {
				b.Logf("%s: series CoV above health-check ceiling %.4g (baseline=%.4g target=%.4g, nivcsw base=%d tgt=%d) -- worth investigating the host, does not invalidate a significant result",
					c.OpcodeID, covCeiling, d.BaselineCoV, d.TargetCoV, base.NivcswDelta, tgt.NivcswDelta)
			}

			runs = append(runs, NewRun(c, d, cfg.Iterations))
		})
	}
	writeRuns(b, runs)
}

func writeRuns(b *testing.B, runs []Run) {
	if p := os.Getenv("GASBENCH_OUT_CSV"); p != "" {
		f, err := os.Create(p)
		if err != nil {
			b.Fatalf("create csv: %v", err)
		}
		defer f.Close()
		if err := WriteCSV(f, runs); err != nil {
			b.Fatalf("write csv: %v", err)
		}
	}
	if p := os.Getenv("GASBENCH_OUT_NDJSON"); p != "" {
		f, err := os.Create(p)
		if err != nil {
			b.Fatalf("create ndjson: %v", err)
		}
		defer f.Close()
		if err := WriteNDJSON(f, runs); err != nil {
			b.Fatalf("write ndjson: %v", err)
		}
	}
}

func envInt(key string, def int) int {
	if v := os.Getenv(key); v != "" {
		if n, err := strconv.Atoi(v); err == nil {
			return n
		}
	}
	return def
}

func envFloat(key string, def float64) float64 {
	if v := os.Getenv(key); v != "" {
		if f, err := strconv.ParseFloat(v, 64); err == nil {
			return f
		}
	}
	return def
}
