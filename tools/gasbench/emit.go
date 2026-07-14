package gasbench

import (
	"encoding/csv"
	"encoding/json"
	"fmt"
	"io"
	"strconv"
)

// Run is one emitted measurement row.
type Run struct {
	InputID    string  `json:"input_id"`
	GasUsed    uint64  `json:"gas_used"`
	ExecTimeNs int64   `json:"exec_time_ns"` // median sample
	Status     string  `json:"status"`
	Iterations int     `json:"iterations"`
	CoV        float64 `json:"cov"`
}

// Status values.
const (
	StatusOK    = "ok"
	StatusNoisy = "noisy" // CoV above floor: measurement not trustworthy
	StatusError = "error"
)

// NewRun builds a Run from a measured series; status is chosen by the caller
// after applying the CoV gate.
func NewRun(s Series, st Stats, status string) Run {
	return Run{
		InputID:    s.InputID,
		GasUsed:    s.GasUsed,
		ExecTimeNs: int64(st.Median),
		Status:     status,
		Iterations: st.N,
		CoV:        st.CoV,
	}
}

var csvHeader = []string{"input_id", "gas_used", "exec_time_ns", "status", "iterations", "cov"}

// WriteCSV writes a header plus one row per run.
func WriteCSV(w io.Writer, runs []Run) error {
	cw := csv.NewWriter(w)
	if err := cw.Write(csvHeader); err != nil {
		return err
	}
	for _, r := range runs {
		rec := []string{
			r.InputID,
			strconv.FormatUint(r.GasUsed, 10),
			strconv.FormatInt(r.ExecTimeNs, 10),
			r.Status,
			strconv.Itoa(r.Iterations),
			strconv.FormatFloat(r.CoV, 'g', 6, 64),
		}
		if err := cw.Write(rec); err != nil {
			return err
		}
	}
	cw.Flush()
	return cw.Error()
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
