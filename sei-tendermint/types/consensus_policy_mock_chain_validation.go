//go:build mock_chain_validation

package types

import "errors"

// ConsensusPolicy here swallows only the sentinels that drift because this build
// cannot reproduce the migration-affected app state or the validator set it
// replays against; the logical-digest comparator is this build's correctness
// signal, not these checks.
//
// Peer-supplied block-content integrity is deliberately NOT swallowed --
// ErrDataHash, ErrEvidenceHash, ErrPerEvidenceValidateBasic still halt -- so a
// malformed or lying peer cannot silently poison the audit input.
type ConsensusPolicy struct{}

// Allowlist (not "ValidationErrors() minus exclusions"): a sentinel added later
// halts by default until it is shown to drift for migration/validator-set reasons
// and added here — the safe default for a consensus-relaxing build.
var swallowedErrors = []error{
	ErrAppHash,
	ErrLastResultsHash,
	ErrLastBlockID,
	ErrConsensusHash,
	ErrValidatorsHash,
	ErrNextValidatorsHash,
	ErrLastCommitVerify,
	ErrLastCommitHash,
	ErrProposerNotInValidatorSet,
	ErrTooMuchEvidence,
	ErrUpgradeBeforeTrigger,
}

func (ConsensusPolicy) HandleError(err error) error {
	for _, e := range swallowedErrors {
		if errors.Is(err, e) {
			recordUnsafeValidationSkipped(err)
			return nil
		}
	}
	return err
}
