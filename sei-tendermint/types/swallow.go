package types

import (
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
)

// unsafeValidationSkippedTotal counts halting validation failures that were
// swallowed by a non-default ConsensusPolicy. Always zero in production builds.
var unsafeValidationSkippedTotal = promauto.NewCounterVec(
	prometheus.CounterOpts{
		Name: "sei_unsafe_validation_skipped_total",
		Help: "Halting validation failures swallowed by a non-default ConsensusPolicy (mock_block_validation, mock_chain_validation). Always zero in production builds.",
	},
	[]string{"kind"},
)
