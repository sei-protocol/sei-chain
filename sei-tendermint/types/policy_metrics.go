package types

import (
	"context"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/metric"

	"github.com/sei-protocol/sei-chain/sei-tendermint/libs/utils"
)

var (
	policyMeter = otel.Meter("sei-tendermint/types")

	unsafeValidationSkippedTotal = utils.OrPanic1(policyMeter.Int64Counter(
		"sei_unsafe_validation_skipped_total",
		metric.WithDescription("Halting validation failures swallowed by a non-default ConsensusPolicy (mock_block_validation, mock_chain_validation). Always zero in production builds."),
	))
)

// recordUnsafeValidationSkipped increments the counter with a kind attribute.
// Called by non-default ConsensusPolicy variants when they swallow a halting
// validation failure.
func recordUnsafeValidationSkipped(kind ErrorKind) {
	unsafeValidationSkippedTotal.Add(context.Background(), 1,
		metric.WithAttributes(attribute.String("kind", string(kind))))
}
