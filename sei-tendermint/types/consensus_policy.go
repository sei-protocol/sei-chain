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
//	                         (M2 deliverable)
//
// Validation failures are modeled as *ConsensusPolicyError sentinels. Call
// sites wrap a contextual message via Err*.With(format, args...); the policy
// matches sentinels with errors.Is, so identity survives %w wrapping.
//
// One Skip*-style early-return is preserved alongside the policy:
// tmtypes.SkipLastResultsHashValidation; see validation.go for context.
package types

import (
	"errors"
	"fmt"
)

// DefaultConsensusPolicy returns the zero-value policy for the current build.
func DefaultConsensusPolicy() ConsensusPolicy { return ConsensusPolicy{} }

// ConsensusPolicyError is a swallow-eligible validation failure. Sentinels of
// this type are compared by their label (see Is), so a contextual error built
// with With still matches its parent sentinel under errors.Is. The label is
// the metric label emitted on sei_unsafe_validation_skipped_total{kind=...}
// and MUST remain byte-identical across refactors.
//
// When a call site formats its message with a %w verb, the wrapped error is
// captured in cause and exposed via Unwrap, so errors.Is/As can still reach
// the underlying typed error (e.g. ErrInvalidCommitHeight, ErrEvidenceOverflow).
type ConsensusPolicyError struct {
	label, msg string
	cause      error
}

// Error returns the human-readable (possibly contextual) message.
func (e *ConsensusPolicyError) Error() string { return e.msg }

// Is reports whether target is a *ConsensusPolicyError with the same label,
// making errors.Is(contextualErr, ErrXxx) true for any With-derived error.
func (e *ConsensusPolicyError) Is(target error) bool {
	t, ok := target.(*ConsensusPolicyError)
	return ok && t.label == e.label
}

// Unwrap exposes the error captured via a %w verb in With, so errors.Is/As
// can traverse to the underlying typed cause. Returns nil when no %w was used.
func (e *ConsensusPolicyError) Unwrap() error { return e.cause }

// Label returns the stable metric label for this kind of failure.
func (e *ConsensusPolicyError) Label() string { return e.label }

// With returns a new error carrying the same label and a formatted message.
// Use at call sites to attach context while preserving errors.Is identity.
// A %w verb in format is honored: the wrapped error is retained for Unwrap,
// so errors.Is/As against the inner error still works.
func (e *ConsensusPolicyError) With(format string, args ...any) *ConsensusPolicyError {
	wrapped := fmt.Errorf(format, args...)
	return &ConsensusPolicyError{
		label: e.label,
		msg:   wrapped.Error(),
		cause: errors.Unwrap(wrapped),
	}
}

// Swallow-eligible validation failure sentinels. The label (first field) is a
// metric label on sei_unsafe_validation_skipped_total{kind=...} and MUST NOT
// change.
var (
	ErrAppHash                   = &ConsensusPolicyError{label: "app_hash", msg: "wrong app hash"}
	ErrDataHash                  = &ConsensusPolicyError{label: "data_hash", msg: "wrong data hash"}
	ErrLastResultsHash           = &ConsensusPolicyError{label: "last_results_hash", msg: "wrong last results hash"}
	ErrLastBlockID               = &ConsensusPolicyError{label: "last_block_id", msg: "wrong last block id"}
	ErrConsensusHash             = &ConsensusPolicyError{label: "consensus_hash", msg: "wrong consensus hash"}
	ErrValidatorsHash            = &ConsensusPolicyError{label: "validators_hash", msg: "wrong validators hash"}
	ErrNextValidatorsHash        = &ConsensusPolicyError{label: "next_validators_hash", msg: "wrong next validators hash"}
	ErrLastCommitVerify          = &ConsensusPolicyError{label: "last_commit_verify", msg: "last commit verification failed"}
	ErrProposerNotInValidatorSet = &ConsensusPolicyError{label: "proposer_not_in_validator_set", msg: "proposer not in validator set"}
	// Kind suffix disambiguates from the existing ErrEvidenceOverflow struct in evidence.go.
	ErrEvidenceOverflowKind     = &ConsensusPolicyError{label: "evidence_overflow", msg: "evidence overflow"}
	ErrLastCommitHash           = &ConsensusPolicyError{label: "last_commit_hash", msg: "wrong last commit hash"}
	ErrEvidenceHash             = &ConsensusPolicyError{label: "evidence_hash", msg: "wrong evidence hash"}
	ErrPerEvidenceValidateBasic = &ConsensusPolicyError{label: "per_evidence_validate_basic", msg: "evidence failed ValidateBasic"}
)

// ValidationErrorKinds returns the audit's swallow-eligible set.
// Tests iterate this list to assert the per-variant matrix.
func ValidationErrorKinds() []*ConsensusPolicyError {
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
		ErrEvidenceOverflowKind,
		ErrLastCommitHash,
		ErrEvidenceHash,
		ErrPerEvidenceValidateBasic,
	}
}
