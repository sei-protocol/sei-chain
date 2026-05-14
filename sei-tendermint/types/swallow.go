package types

import (
	"fmt"
	"log/slog"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
)

// unsafeValidationSkippedTotal counts every halting validation failure
// that ConsensusPolicy chose to swallow. Labeled by site (the calling
// file:line, hand-coded at each call) and kind (the ErrorKind constant
// string). Lives at the default Prometheus registry — production builds
// never increment it because ShouldSwallow returns false on all kinds.
//
// Metric name follows the audit's specification:
// sei_unsafe_validation_skipped_total{site, kind}.
var unsafeValidationSkippedTotal = promauto.NewCounterVec(
	prometheus.CounterOpts{
		Name: "sei_unsafe_validation_skipped_total",
		Help: "Halting validation failures swallowed by a non-default ConsensusPolicy (mock_block_validation, mock_chain_validation). Always zero in production builds.",
	},
	[]string{"site", "kind"},
)

// LogSwallowedFailure emits the structured log line that accompanies a
// swallowed validation failure. Every call site uses this so the operator
// sees a uniform record of (kind, site, height, expected, got). The
// counter increment is the responsibility of SwallowOrErr — this function
// only logs.
//
// If the logger is nil it falls back to the types package logger
// (declared in part_set.go) so call sites that don't carry one (such as
// Block.ValidateBasic) can still produce a record.
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

// SwallowOrErr applies the ConsensusPolicy failure-handling decision at a
// halting validation site. Call it from the failure branch (i.e. when
// the comparison has already failed); it returns nil to swallow (logging
// + counter) or a formatted error to halt.
//
// Typical use:
//
//	if !bytes.Equal(block.AppHash, state.AppHash) {
//	    if err := types.SwallowOrErr(policy, types.ErrorKindAppHash, logger, site,
//	        block.Height, state.AppHash, block.AppHash,
//	        "wrong Block.Header.AppHash. Expected %X, got %v",
//	        state.AppHash, block.AppHash); err != nil {
//	        return err
//	    }
//	}
//
// Skip-before-compute is intentionally no longer supported — every check
// computes its comparison authentically; only the failure-handling
// differs per ConsensusPolicy.
//
// Parameters:
//   - policy: the per-binary ConsensusPolicy (build-tag-selected).
//   - kind:   the audit-row ErrorKind being checked.
//   - lg:     *slog.Logger to receive the divergence record. nil falls
//     back to the types package logger.
//   - site:   file:line of the calling check (helps trace which audit row
//     fired in metrics + logs).
//   - height: block height of the failing block.
//   - expected, got: the two values being compared. Logged hex-formatted.
//   - format, args: the original fmt.Errorf format the call site would
//     have used to halt. Returned on the halt path.
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
