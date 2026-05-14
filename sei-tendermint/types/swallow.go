package types

import (
	"fmt"
	"log/slog"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
)

// unsafeValidationSkippedTotal — production builds never increment this
// (ShouldSwallow returns false on all kinds and the increment is DCE'd).
// Non-zero values indicate a mock_* tag is in effect.
var unsafeValidationSkippedTotal = promauto.NewCounterVec(
	prometheus.CounterOpts{
		Name: "sei_unsafe_validation_skipped_total",
		Help: "Halting validation failures swallowed by a non-default ConsensusPolicy (mock_block_validation, mock_chain_validation). Always zero in production builds.",
	},
	[]string{"site", "kind"},
)

// LogSwallowedFailure emits the structured record for a swallowed failure.
// nil logger falls back to the package logger so call sites without one
// (e.g. Block.ValidateBasic) can still produce a record.
func LogSwallowedFailure(
	lg *slog.Logger,
	kind ErrorKind,
	site string,
	height int64,
	expected, got interface{},
) {
	if lg == nil {
		lg = logger
	}
	lg.Warn("swallowed validation failure (unsafe ConsensusPolicy)",
		"kind", string(kind),
		"site", site,
		"height", height,
		"expected", fmt.Sprintf("%X", expected),
		"got", fmt.Sprintf("%X", got),
	)
}

// SwallowOrErr is the contract every halting check funnels through.
// Call from the failure branch (comparison already failed):
//
//   - returns nil → caller continues past the failure (logged + counter
//     incremented).
//   - returns a formatted error → caller halts with that error.
//
// Pre-comparison short-circuits are intentionally not supported — every
// check computes its comparison; only the failure handling is policy-driven.
func SwallowOrErr(
	policy ConsensusPolicy,
	kind ErrorKind,
	lg *slog.Logger,
	site string,
	height int64,
	expected, got interface{},
	format string,
	args ...interface{},
) error {
	if policy.ShouldSwallow(kind) {
		LogSwallowedFailure(lg, kind, site, height, expected, got)
		unsafeValidationSkippedTotal.WithLabelValues(site, string(kind)).Inc()
		return nil
	}
	return fmt.Errorf(format, args...)
}
