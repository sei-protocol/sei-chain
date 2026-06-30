//go:build mock_block_validation || mock_chain_validation

package types

import (
	"context"
	"errors"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/metric"

	"github.com/sei-protocol/sei-chain/sei-tendermint/libs/utils"
)

var (
	policyMeter = otel.Meter("sei-tendermint/types")

	unsafeValidationSkippedTotal = utils.OrPanic1(policyMeter.Int64Counter(
		"sei_unsafe_validation_skipped_total",
		metric.WithDescription("Halting validation failures swallowed by a non-default ConsensusPolicy (mock_block_validation, mock_chain_validation)."),
	))
)

// validationLabel maps a swallowed validation error to its metric label by
// sentinel identity — rendering errors as strings is the metric layer's job,
// not the error type's. Values are the {validation_error=...} contract.
func validationLabel(err error) string {
	switch {
	case errors.Is(err, ErrAppHash):
		return "app_hash"
	case errors.Is(err, ErrDataHash):
		return "data_hash"
	case errors.Is(err, ErrLastResultsHash):
		return "last_results_hash"
	case errors.Is(err, ErrLastBlockID):
		return "last_block_id"
	case errors.Is(err, ErrConsensusHash):
		return "consensus_hash"
	case errors.Is(err, ErrValidatorsHash):
		return "validators_hash"
	case errors.Is(err, ErrNextValidatorsHash):
		return "next_validators_hash"
	case errors.Is(err, ErrLastCommitVerify):
		return "last_commit_verify"
	case errors.Is(err, ErrProposerNotInValidatorSet):
		return "proposer_not_in_validator_set"
	case errors.Is(err, ErrTooMuchEvidence):
		return "too_much_evidence"
	case errors.Is(err, ErrLastCommitHash):
		return "last_commit_hash"
	case errors.Is(err, ErrEvidenceHash):
		return "evidence_hash"
	case errors.Is(err, ErrPerEvidenceValidateBasic):
		return "per_evidence_validate_basic"
	case errors.Is(err, ErrUpgradeBeforeTrigger):
		return "upgrade_before_trigger"
	default:
		return "unknown"
	}
}

// recordUnsafeValidationSkipped increments the counter, labeled via
// validationLabel. Called by the mock policies when they swallow a failure.
func recordUnsafeValidationSkipped(err error) {
	unsafeValidationSkippedTotal.Add(context.Background(), 1,
		metric.WithAttributes(attribute.String("validation_error", validationLabel(err))))
}
