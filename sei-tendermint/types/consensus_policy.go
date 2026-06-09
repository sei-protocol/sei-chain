// Package types — ConsensusPolicy is a zero-sized, build-tag-selected gate
// that decides, per validation failure, whether a halting validation failure
// halts (default) or is swallowed (counter incremented, then continued). The
// single method HandleError(err) is declared in exactly one of three per-tag
// files, so each binary compiles in one fixed policy with no runtime branch:
//
//	default (production)   → returns err for every failure; production halting
//	                         semantics are unchanged
//	mock_block_validation  → returns nil for ErrAppHash and ErrDataHash;
//	                         preserves the long-standing behavior of that tag
//	mock_chain_validation  → returns nil for every swallow-eligible audit-row
//	                         sentinel except ErrLastCommitVerify, excluded to
//	                         avoid a downstream buildLastCommitInfo panic
//
// Validation failures are modeled as sentinel errors. Call sites attach context
// with idiomatic fmt.Errorf("...: %w", ErrX): wrapping keeps errors.Is(err, ErrX)
// true by identity, which is how each per-tag policy decides whether to swallow.
// Sites that must keep an inner typed error reachable too use multi-%w
// (fmt.Errorf("%w: %w", ErrX, inner)). The sentinel→metric-label mapping lives in
// the metric reporting subsystem (policy_metrics.go), not on the error type.
//
// One Skip*-style early-return is preserved alongside the policy:
// tmtypes.SkipLastResultsHashValidation; see validation.go for context.
package types

import "errors"

// DefaultConsensusPolicy returns the zero-value policy for the current build.
func DefaultConsensusPolicy() ConsensusPolicy { return ConsensusPolicy{} }

// Swallow-eligible validation failure sentinels. Each is matched by identity
// via errors.Is, so call sites must wrap (not replace) them with %w.
var (
	ErrAppHash                   = errors.New("app hash mismatch")
	ErrDataHash                  = errors.New("data hash mismatch")
	ErrLastResultsHash           = errors.New("last results hash mismatch")
	ErrLastBlockID               = errors.New("last block ID mismatch")
	ErrConsensusHash             = errors.New("consensus hash mismatch")
	ErrValidatorsHash            = errors.New("validators hash mismatch")
	ErrNextValidatorsHash        = errors.New("next validators hash mismatch")
	ErrLastCommitVerify          = errors.New("last commit verification failed")
	ErrProposerNotInValidatorSet = errors.New("proposer not in validator set")
	// Distinct from the ErrEvidenceOverflow struct in evidence.go, which carries
	// Max/Got and rides along as the inner %w cause; this is the stable identity
	// used for the swallow decision and the metric label.
	ErrTooMuchEvidence          = errors.New("evidence size exceeds limit")
	ErrLastCommitHash           = errors.New("last commit hash mismatch")
	ErrEvidenceHash             = errors.New("evidence hash mismatch")
	ErrPerEvidenceValidateBasic = errors.New("evidence failed ValidateBasic")
)

// ValidationErrors returns the audit's swallow-eligible sentinel set.
// Tests iterate this list to assert the per-variant matrix.
func ValidationErrors() []error {
	return []error{
		ErrAppHash,
		ErrDataHash,
		ErrLastResultsHash,
		ErrLastBlockID,
		ErrConsensusHash,
		ErrValidatorsHash,
		ErrNextValidatorsHash,
		ErrLastCommitVerify,
		ErrProposerNotInValidatorSet,
		ErrTooMuchEvidence,
		ErrLastCommitHash,
		ErrEvidenceHash,
		ErrPerEvidenceValidateBasic,
	}
}
