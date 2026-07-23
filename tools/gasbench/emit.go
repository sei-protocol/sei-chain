package gasbench

import (
	"encoding/csv"
	"encoding/json"
	"fmt"
	"io"
	"math"
	"strconv"
)

// Run is one emitted measurement row: the per-opcode aggregate over all
// -count passes, ready for a gas-vs-time correlation. See README.md "Output
// schema".
type Run struct {
	InputID    string  `json:"input_id"`     // opcode id, e.g. "ADD"
	Class      string  `json:"class"`        // opcode family, e.g. "arithmetic"
	Reps       int     `json:"reps"`         // opcode executions the delta represents
	GasUsed    uint64  `json:"gas_used"`     // whole-program gas delta (target - baseline); per-op = GasUsed/Reps -- this is the DIFFERENTIAL (net of filler), not the gas the chain charges
	ConstGas   uint64  `json:"const_gas"`    // nominal per-op gas the chain charges (jump-table ConstGas); the repricing-relevant denominator for ns-per-gas. See README.md "Read the output"
	ExecTimeNs float64 `json:"exec_time_ns"` // median whole-program delta over the counts (Summary.Center); always finite; per-op = ExecTimeNs/Reps
	Count      int     `json:"count"`        // cross-run n: number of -count passes aggregated
	Iterations int     `json:"iterations"`   // timed iterations behind each series

	// CILo/CIHi are the order-statistic CI bounds on ExecTimeNs (whole-program
	// ns). Nullable: nil (JSON null / CSV empty) when the raw benchmath bound
	// is non-finite (underpowered). THE GATE (Distinguishable) reads the raw
	// bounds, never these pointers -- see crossrun.go and distinguishable.
	CILo            *float64 `json:"ci_lo"`
	CIHi            *float64 `json:"ci_hi"`
	Confidence      float64  `json:"confidence"`       // actual CI confidence (>= requested)
	Distinguishable bool     `json:"distinguishable"`  // paired delta CI excludes zero (ci_lo>0 || ci_hi<0) -- the statistical half of the gate (F1)
	EffectSizePass  bool     `json:"effect_size_pass"` // per-op median delta clears the effect floor -- the practical half of the gate
	PValue          float64  `json:"p_value"`          // advisory only: unpaired Mann-Whitney U p over baseline vs target medians; NOT the gate
	Alpha           float64  `json:"alpha"`            // CompareAlpha behind the advisory p_value
	Status          string   `json:"status"`           // ok | sub_threshold | insignificant | underpowered | error -- never gated on CoV

	CoV          float64 `json:"cov"`           // worse (max) of the baseline/target series CoV, max across counts -- advisory only
	HighVariance bool    `json:"high_variance"` // advisory: CoV exceeded the health-check ceiling on any count; does not invalidate Status=ok
	Nvcsw        int64   `json:"nvcsw"`         // worse (max) of the baseline/target voluntary-context-switch count, max across counts; see README.md "Active-benchmarking diagnostics"
	Nivcsw       int64   `json:"nivcsw"`        // worse (max) of the baseline/target involuntary-context-switch count, max across counts; see README.md "Active-benchmarking diagnostics"
}

// Status values. See README.md "Acceptance gate" for why Status reads the
// paired delta CI (Distinguishable), not CoV.
const (
	StatusOK            = "ok"            // distinguishable AND clears the effect-size floor
	StatusSubThreshold  = "sub_threshold" // distinguishable but below the effect floor -- resolvable, but not meaningfully more expensive in ABSOLUTE time. NOT "correctly priced": a cheap op can still be mispriced. Only `ok` is correlation-eligible.
	StatusInsignificant = "insignificant" // measurable but the CI straddles zero -- not distinguishable at this precision
	StatusUnderpowered  = "underpowered"  // too few counts for a finite CI (count<2 or non-finite bound); the verdict short-circuits before distinguishability
	// StatusError marks a case whose measurement never happened: its subtest
	// b.Fatalf'd (invalid program, Measure failure), which unwinds only that
	// subtest -- the parent still aggregates, and an empty accumulator must
	// become an error row (NewErrorRun), never NaN in the output.
	StatusError = "error"
)

// distinguishable is the statistical half of the acceptance gate (F1): the
// paired delta CI excludes zero. It reads the RAW float64 bounds from crossRun
// (never the null-mapped *float64), so a non-finite bound is ±Inf-safe -- -Inf
// is not > 0 and +Inf is not < 0, so an underpowered row is never
// distinguishable and no nil deref is possible. It is deliberately NOT the
// advisory p_value.
func distinguishable(ciLo, ciHi float64) bool { return ciLo > 0 || ciHi < 0 }

// effectSizePass is the practical half of the gate: the per-op median delta
// clears a minimum absolute-ns floor, so "significant" means "meaningfully more
// expensive," not merely "distinguishable at large n". A minPerOpNs <= 0
// disables the floor (every distinguishable row passes). The gas-relative floor
// is deferred (see README/design).
func effectSizePass(perOpDeltaNs, minPerOpNs float64) bool {
	return minPerOpNs <= 0 || math.Abs(perOpDeltaNs) >= minPerOpNs
}

// classifyStatus derives the verdict. Underpowered short-circuits first (its CI
// is non-finite, so distinguishability is not meaningful). Acceptance
// (StatusOK) requires BOTH distinguishable and effectPass; distinguishable but
// below the floor is StatusSubThreshold. CoV/HighVariance never enter here.
func classifyStatus(underpowered, distinguishable, effectPass bool) string {
	switch {
	case underpowered:
		return StatusUnderpowered
	case distinguishable && effectPass:
		return StatusOK
	case distinguishable:
		return StatusSubThreshold
	default:
		return StatusInsignificant
	}
}

// nullableBound maps a raw CI bound to the wire's nullable column: nil when
// non-finite (D-1: neither ±Inf nor NaN may reach encoding/json — it rejects
// both). This is the only place raw->nullable happens; the gate reads the raw
// bound upstream.
func nullableBound(x float64) *float64 {
	if !finite(x) {
		return nil
	}
	return &x
}

// finite reports whether x is a real number (not ±Inf, not NaN).
func finite(x float64) bool { return !math.IsInf(x, 0) && !math.IsNaN(x) }

// NewErrorRun is the row for a case whose measurement never happened (its
// subtest failed before accumulating a single count). Every numeric field is
// finite/zero and the CI is null, so the writers stay NaN-free (D-1) and a
// consumer filtering on status sees the failure instead of a corrupt number.
func NewErrorRun(c Case, iterations int) Run {
	return Run{
		InputID:    c.OpcodeID,
		Class:      string(c.Class),
		Reps:       c.Reps,
		ConstGas:   c.ConstGas,
		Iterations: iterations,
		PValue:     1,
		Status:     StatusError,
	}
}

// hostHealth carries the advisory host-health signals for one opcode: the
// max-across-counts values from the collector. Named fields (like crossRunInput)
// keep the same-typed Nvcsw/Nivcsw from transposing at the NewRun call site.
type hostHealth struct {
	CoV          float64 // worse-of-pair within-run CoV, max across counts
	HighVariance bool    // CoV exceeded the ceiling on any count
	Nvcsw        int64   // worse-of-pair voluntary ctx switches, max across counts
	Nivcsw       int64   // worse-of-pair involuntary ctx switches, max across counts
}

// NewRun composes the per-opcode aggregate row. cr carries the cross-run
// benchmath verdict (raw CI bounds); h carries the advisory host-health signals;
// minPerOpNs is the effect-size floor (<=0 disables it). The gate reads cr's raw
// bounds; the wire gets null-mapped ones.
func NewRun(c Case, cr crossRun, gasUsed uint64, count, iterations int, h hostHealth, minPerOpNs float64) Run {
	// dist/effect (not the package funcs) to avoid shadowing.
	dist := distinguishable(cr.CILo, cr.CIHi)
	effect := effectSizePass(cr.MedianDeltaNs/float64(c.Reps), minPerOpNs)
	return Run{
		InputID:         c.OpcodeID,
		Class:           string(c.Class),
		Reps:            c.Reps,
		GasUsed:         gasUsed,
		ConstGas:        c.ConstGas,
		ExecTimeNs:      cr.MedianDeltaNs,
		Count:           count,
		Iterations:      iterations,
		CILo:            nullableBound(cr.CILo),
		CIHi:            nullableBound(cr.CIHi),
		Confidence:      cr.Confidence,
		Distinguishable: dist,
		EffectSizePass:  effect,
		PValue:          cr.P,
		Alpha:           cr.Alpha,
		Status:          classifyStatus(cr.Underpowered, dist, effect),
		CoV:             h.CoV,
		HighVariance:    h.HighVariance,
		Nvcsw:           h.Nvcsw,
		Nivcsw:          h.Nivcsw,
	}
}

var csvHeader = []string{"input_id", "class", "reps", "gas_used", "const_gas", "exec_time_ns", "count", "iterations", "ci_lo", "ci_hi", "confidence", "distinguishable", "effect_size_pass", "p_value", "alpha", "status", "cov", "high_variance", "nvcsw", "nivcsw"}

// formatBound renders a nullable CI bound: empty for a null (underpowered)
// bound, full precision otherwise.
func formatBound(p *float64) string {
	if p == nil {
		return ""
	}
	return strconv.FormatFloat(*p, 'g', -1, 64)
}

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
			strconv.FormatUint(r.ConstGas, 10),
			strconv.FormatFloat(r.ExecTimeNs, 'g', -1, 64),
			strconv.Itoa(r.Count),
			strconv.Itoa(r.Iterations),
			// A null bound (underpowered) renders as an empty field; older CSV
			// artifacts wrote +Inf here. See README.md schema.
			formatBound(r.CILo),
			formatBound(r.CIHi),
			strconv.FormatFloat(r.Confidence, 'g', -1, 64),
			strconv.FormatBool(r.Distinguishable),
			strconv.FormatBool(r.EffectSizePass),
			strconv.FormatFloat(r.PValue, 'g', -1, 64),
			strconv.FormatFloat(r.Alpha, 'g', -1, 64),
			r.Status,
			// CoV is advisory-only (README.md "Acceptance gate"), so this
			// deliberately rounds to 6 sig figs for readability; NDJSON's
			// json.Encoder gives CoV full precision instead -- a consumer
			// comparing CoV across the two formats will see this difference.
			strconv.FormatFloat(r.CoV, 'g', 6, 64),
			strconv.FormatBool(r.HighVariance),
			strconv.FormatInt(r.Nvcsw, 10),
			strconv.FormatInt(r.Nivcsw, 10),
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
