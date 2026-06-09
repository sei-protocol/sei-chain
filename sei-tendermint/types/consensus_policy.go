// Package types — ConsensusPolicy is a zero-sized, build-tag-selected gate
// that decides, per validation failure, whether a halting validation failure
// halts (default) or is swallowed (counter incremented, then continued). The
// single method HandleError(err) is declared in exactly one of three per-tag
// files, so each binary compiles in one fixed policy with no runtime branch:
//
//	default (production)   → returns err for every kind; production halting
//	                         semantics are unchanged
//	mock_block_validation  → returns nil for ErrAppHash and ErrDataHash;
//	                         preserves the long-standing behavior of that tag
//	mock_chain_validation  → returns nil for every swallow-eligible audit-row
//	                         sentinel except ErrLastCommitVerify, excluded to
//	                         avoid a downstream buildLastCommitInfo panic
//
// Validation failures are modeled as *ConsensusPolicyError sentinels. Call
// sites attach context with idiomatic fmt.Errorf("...: %w", ErrX): wrapping
// the sentinel pointer keeps errors.Is(err, ErrX) true by identity and lets
// recordUnsafeValidationSkipped recover the sentinel — and its Code label —
// via errors.As. Sites that must keep an inner typed error reachable too use
// multi-%w (fmt.Errorf("%w: %w", ErrX, inner)).
//
// One Skip*-style early-return is preserved alongside the policy:
// tmtypes.SkipLastResultsHashValidation; see validation.go for context.
package types

// DefaultConsensusPolicy returns the zero-value policy for the current build.
func DefaultConsensusPolicy() ConsensusPolicy { return ConsensusPolicy{} }

// ConsensusPolicyError is a swallow-eligible validation failure. Code is the
// stable metric label on sei_unsafe_validation_skipped_total{kind=...} and
// MUST NOT change; it is also the sentinel's error string. Matched via
// errors.Is by identity; recovered via errors.As. Call sites supply the
// human-readable context through the wrapping fmt.Errorf.
type ConsensusPolicyError struct{ Code string }

// Error returns the Code, which doubles as the sentinel's message.
func (e *ConsensusPolicyError) Error() string { return e.Code }

// Swallow-eligible validation failure sentinels. The Code field is a metric
// label on sei_unsafe_validation_skipped_total{kind=...} and MUST NOT change.
var (
	ErrAppHash                   = &ConsensusPolicyError{Code: "app_hash"}
	ErrDataHash                  = &ConsensusPolicyError{Code: "data_hash"}
	ErrLastResultsHash           = &ConsensusPolicyError{Code: "last_results_hash"}
	ErrLastBlockID               = &ConsensusPolicyError{Code: "last_block_id"}
	ErrConsensusHash             = &ConsensusPolicyError{Code: "consensus_hash"}
	ErrValidatorsHash            = &ConsensusPolicyError{Code: "validators_hash"}
	ErrNextValidatorsHash        = &ConsensusPolicyError{Code: "next_validators_hash"}
	ErrLastCommitVerify          = &ConsensusPolicyError{Code: "last_commit_verify"}
	ErrProposerNotInValidatorSet = &ConsensusPolicyError{Code: "proposer_not_in_validator_set"}
	// Code suffix disambiguates from the existing ErrEvidenceOverflow struct in evidence.go.
	ErrEvidenceOverflowCode     = &ConsensusPolicyError{Code: "evidence_overflow"}
	ErrLastCommitHash           = &ConsensusPolicyError{Code: "last_commit_hash"}
	ErrEvidenceHash             = &ConsensusPolicyError{Code: "evidence_hash"}
	ErrPerEvidenceValidateBasic = &ConsensusPolicyError{Code: "per_evidence_validate_basic"}
)

// ValidationErrors returns the audit's swallow-eligible set.
// Tests iterate this list to assert the per-variant matrix.
func ValidationErrors() []*ConsensusPolicyError {
	return []*ConsensusPolicyError{
		ErrAppHash,
		ErrDataHash,
		ErrLastResultsHash,
		ErrLastBlockID,
		ErrConsensusHash,
		ErrValidatorsHash,
		ErrNextValidatorsHash,
		ErrLastCommitVerify,
		ErrProposerNotInValidatorSet,
		ErrEvidenceOverflowCode,
		ErrLastCommitHash,
		ErrEvidenceHash,
		ErrPerEvidenceValidateBasic,
	}
}
