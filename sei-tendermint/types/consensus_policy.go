// Package types — ConsensusPolicy is a zero-sized, build-tag-selected gate
// that decides, per ErrorKind, whether a halting validation failure halts
// (default) or is swallowed (counter incremented, then continued). The
// single method ShouldSwallow(kind, err) is declared in exactly one of three
// per-tag files, so each binary compiles in one fixed policy with no runtime
// branch:
//
//	default (production)   → returns err for every kind; production halting
//	                         semantics are unchanged
//	mock_block_validation  → returns nil for AppHash and DataHash; preserves
//	                         the long-standing behavior of that tag
//	mock_chain_validation  → returns nil for every swallow-eligible audit-row
//	                         kind (M2 deliverable)
//
// One Skip*-style early-return is preserved alongside the policy:
// tmtypes.SkipLastResultsHashValidation; see validation.go for context.
package types

// DefaultConsensusPolicy returns the zero-value policy for the current build.
func DefaultConsensusPolicy() ConsensusPolicy { return ConsensusPolicy{} }

// ErrorKind names a swallow-eligible validation failure. Constants
// correspond to rows in the M1.0 audit (docs/designs/
// mock-chain-validation-m1-audit.md); the string value is the metric label
// emitted on sei_unsafe_validation_skipped_total{kind=...}.
type ErrorKind string

const (
	ErrorKindAppHash                   ErrorKind = "app_hash"
	ErrorKindDataHash                  ErrorKind = "data_hash"
	ErrorKindLastResultsHash           ErrorKind = "last_results_hash"
	ErrorKindLastBlockID               ErrorKind = "last_block_id"
	ErrorKindConsensusHash             ErrorKind = "consensus_hash"
	ErrorKindValidatorsHash            ErrorKind = "validators_hash"
	ErrorKindNextValidatorsHash        ErrorKind = "next_validators_hash"
	ErrorKindLastCommitVerify          ErrorKind = "last_commit_verify"
	ErrorKindProposerNotInValidatorSet ErrorKind = "proposer_not_in_validator_set"
	ErrorKindEvidenceOverflow          ErrorKind = "evidence_overflow"
	ErrorKindLastCommitHash            ErrorKind = "last_commit_hash"
	ErrorKindEvidenceHash              ErrorKind = "evidence_hash"
	ErrorKindPerEvidenceValidateBasic  ErrorKind = "per_evidence_validate_basic"
)

// AllSwallowEligibleErrorKinds returns the audit's swallow-eligible set.
// Tests iterate this list to assert the per-variant matrix.
func AllSwallowEligibleErrorKinds() []ErrorKind {
	return []ErrorKind{
		ErrorKindAppHash,
		ErrorKindDataHash,
		ErrorKindLastResultsHash,
		ErrorKindLastBlockID,
		ErrorKindConsensusHash,
		ErrorKindValidatorsHash,
		ErrorKindNextValidatorsHash,
		ErrorKindLastCommitVerify,
		ErrorKindProposerNotInValidatorSet,
		ErrorKindEvidenceOverflow,
		ErrorKindLastCommitHash,
		ErrorKindEvidenceHash,
		ErrorKindPerEvidenceValidateBasic,
	}
}
