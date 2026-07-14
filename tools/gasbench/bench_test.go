package gasbench

import (
	"os"
	"strconv"
	"testing"
)

// BenchmarkOpcodes drives one differential measurement per scalar opcode
// Case. Run with -benchtime=1x (the inner loop is Measure's, not b.N) and
// -count=K to get K independent process runs for benchstat cross-run
// variance.
func BenchmarkOpcodes(b *testing.B) {
	cases := BuildCases()
	cfg := Config{
		Warmup:     envInt("GASBENCH_WARMUP", 2000),
		Iterations: envInt("GASBENCH_ITERS", 20000),
		DisableGC:  true,
		LockThread: true,
	}
	sigmaK := envFloat("GASBENCH_SIGMA_K", 3)
	covFloor := envFloat("GASBENCH_COV_FLOOR", 0.02)

	var runs []Run
	for _, c := range cases {
		c := c
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

			bStats := Summarize(base.Samples)
			tStats := Summarize(tgt.Samples)
			d := Subtract(c.OpcodeID, base, tgt, c.Reps, sigmaK, covFloor)

			// Self-check of the differential construction: the measured
			// whole-program gas delta must equal the definitional expected
			// delta, or the baseline/target pair is not isolating the
			// opcode the way BuildCaseWith intends. This is a correctness
			// check on the harness, not on opcode timing.
			if d.GasDelta != c.ExpectedGasDelta {
				b.Errorf("%s: gas self-check failed: measured delta %d != expected %d (baseline/target pair does not isolate the opcode)",
					c.OpcodeID, d.GasDelta, c.ExpectedGasDelta)
			}

			b.ReportMetric(tStats.Median, "median-ns")
			b.ReportMetric(tStats.P99, "p99-ns")
			b.ReportMetric(tStats.CoV, "cov")
			b.ReportMetric(d.PerOpNs, "per-op-ns")
			b.ReportMetric(d.PerOpGas, "per-op-gas")

			for _, w := range append(base.Warnings, tgt.Warnings...) {
				b.Logf("%s: %s", c.OpcodeID, w)
			}
			if !d.Significant {
				b.Logf("%s: delta %.1fns within noise (uncertainty %.1fns, %gσ)",
					c.OpcodeID, d.DeltaNs, d.Uncertainty, sigmaK)
			}
			status := StatusOK
			if !d.NoiseOK {
				status = StatusNoisy
			}
			runs = append(runs,
				NewRun(base, bStats, statusOf(bStats.CoV, covFloor)),
				NewRun(tgt, tStats, status))
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

func statusOf(cov, floor float64) string {
	if cov > floor {
		return StatusNoisy
	}
	return StatusOK
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
