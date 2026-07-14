package gasbench

import (
	"encoding/csv"
	"encoding/json"
	"fmt"
	"io"
	"strconv"
)

// Run is one emitted measurement row: the per-opcode differential result,
// ready for a gas-vs-time correlation. This is the record the design's
// acceptance criteria describe (per-input gas_used and exec_time_ns,
// supporting both a point lookup and a cross-input regression) -- for this
// harness "input" is the opcode, and the values that answer "does gas track
// time" are the differential (target-minus-baseline) gas and time, not
// either series' raw median.
type Run struct {
	InputID     string  `json:"input_id"`     // opcode id, e.g. "ADD"
	Class       string  `json:"class"`        // opcode family, e.g. "arithmetic"
	Reps        int     `json:"reps"`         // opcode executions the delta represents
	GasUsed     uint64  `json:"gas_used"`     // whole-program gas delta (target - baseline); per-op = GasUsed/Reps
	ExecTimeNs  float64 `json:"exec_time_ns"` // whole-program time delta, ns (target median - baseline median); per-op = ExecTimeNs/Reps
	Status      string  `json:"status"`       // ok only if both series clear the noise floor AND the delta is statistically significant
	Iterations  int     `json:"iterations"`   // timed iterations behind each series
	CoV         float64 `json:"cov"`          // worse (max) of the baseline/target series CoV
	Significant bool    `json:"significant"`  // |delta| exceeds SigmaK * propagated uncertainty
}

// Status values.
const (
	StatusOK    = "ok"
	StatusNoisy = "noisy" // a series' CoV is above the floor: measurement not trustworthy
	// StatusInsignificant marks a case whose series are individually stable
	// (NoiseOK) but whose delta does not clear measurement uncertainty --
	// the opcode's marginal cost may be real but is not distinguishable from
	// zero at this precision. Distinct from StatusNoisy: re-running a noisy
	// case may help, re-running an insignificant one on the same host will
	// not -- it needs more Iterations or a coarser SigmaK, not a quieter host.
	StatusInsignificant = "insignificant"
	// StatusError is reserved for a per-case measurement failure. Not
	// currently produced: bench_test.go fails the whole benchmark loudly
	// (b.Fatalf) on an invalid program rather than degrading to an error
	// row, so every emitted Run today is OK/Noisy/Insignificant.
	StatusError = "error"
)

// NewRun builds a Run from a differential result. c is the Case that
// produced d (for its Class); iterations is the sample count behind each
// series (baseline and target share one Config, so a single value applies
// to both).
func NewRun(c Case, d Diff, iterations int) Run {
	cov := d.BaselineCoV
	if d.TargetCoV > cov {
		cov = d.TargetCoV
	}
	status := StatusOK
	switch {
	case !d.NoiseOK:
		status = StatusNoisy
	case !d.Significant:
		status = StatusInsignificant
	}
	return Run{
		InputID:     d.OpcodeID,
		Class:       string(c.Class),
		Reps:        d.Reps,
		GasUsed:     d.GasDelta,
		ExecTimeNs:  d.DeltaNs,
		Status:      status,
		Iterations:  iterations,
		CoV:         cov,
		Significant: d.Significant,
	}
}

var csvHeader = []string{"input_id", "class", "reps", "gas_used", "exec_time_ns", "status", "iterations", "cov", "significant"}

// WriteCSV writes a header plus one row per run.
func WriteCSV(w io.Writer, runs []Run) error {
	cw := csv.NewWriter(w)
	if err := cw.Write(csvHeader); err != nil {
		return fmt.Errorf("gasbench: write csv header: %w", err)
	}
	for i, r := range runs {
		rec := []string{
			r.InputID,
			r.Class,
			strconv.Itoa(r.Reps),
			strconv.FormatUint(r.GasUsed, 10),
			strconv.FormatFloat(r.ExecTimeNs, 'g', -1, 64),
			r.Status,
			strconv.Itoa(r.Iterations),
			strconv.FormatFloat(r.CoV, 'g', 6, 64),
			strconv.FormatBool(r.Significant),
		}
		if err := cw.Write(rec); err != nil {
			return fmt.Errorf("gasbench: write csv row %d: %w", i, err)
		}
	}
	cw.Flush()
	if err := cw.Error(); err != nil {
		return fmt.Errorf("gasbench: flush csv: %w", err)
	}
	return nil
}

// WriteNDJSON writes one JSON object per line.
func WriteNDJSON(w io.Writer, runs []Run) error {
	enc := json.NewEncoder(w) // Encode appends a newline: NDJSON by construction
	for i := range runs {
		if err := enc.Encode(&runs[i]); err != nil {
			return fmt.Errorf("gasbench: encode run %d: %w", i, err)
		}
	}
	return nil
}
