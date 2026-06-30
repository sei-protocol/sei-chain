// Package types — ConsensusPolicy is a zero-sized, build-tag-selected gate
// that decides, per validation failure, whether a halting validation failure
// halts (default) or is swallowed (counter incremented, then continued). Its
// HandleError(err) method is declared in exactly one of three per-tag files, so
// each binary compiles in one fixed policy with no runtime branch:
//
//	default (production)   → returns err for every failure; production halting
//	                         semantics are unchanged
//	mock_block_validation  → returns nil for ErrAppHash, ErrDataHash, and
//	                         ErrUpgradeBeforeTrigger
//	mock_chain_validation  → returns nil for every audit-row sentinel except the
//	                         peer-content-integrity trio (ErrDataHash,
//	                         ErrEvidenceHash, ErrPerEvidenceValidateBasic), which
//	                         still halt; the swallowed set includes
//	                         ErrLastCommitVerify, whose commit/validator-set drift
//	                         buildLastCommitInfo tolerates
//
// Validation failures are modeled as *ConsensusPolicyError sentinels. Call sites
// attach context with idiomatic fmt.Errorf("...: %w", ErrX): wrapping keeps
// errors.Is(err, ErrX) true by identity (how each per-tag policy decides whether
// to swallow), while errors.As recovers the whole class into a
// *ConsensusPolicyError. Sites that must keep an inner typed error reachable too use
// multi-%w (fmt.Errorf("%w: %w", ErrX, inner)). The sentinel→metric-label
// mapping lives in the metric reporting subsystem (policy_metrics.go), not on
// the error type.
//
// One Skip*-style early-return is preserved alongside the policy:
// tmtypes.SkipLastResultsHashValidation; see validation.go for context.
package types

// DefaultConsensusPolicy returns the zero-value policy for the current build.
func DefaultConsensusPolicy() ConsensusPolicy { return ConsensusPolicy{} }

// ConsensusPolicyError is the concrete type of every swallow-eligible validation
// sentinel. Match a specific failure with errors.Is(err, ErrAppHash); detect the
// whole class with errors.As into a *ConsensusPolicyError. msg is the
// human-readable cause — the mapping from a sentinel to its metric label is the
// metric subsystem's concern (policy_metrics.go), deliberately not a field here.
type ConsensusPolicyError struct{ msg string }

func (e *ConsensusPolicyError) Error() string { return e.msg }

// Swallow-eligible validation failure sentinels. Matched by identity via
// errors.Is and recoverable as a class via errors.As; call sites must wrap
// (not replace) them with %w.
var (
	ErrAppHash                   = &ConsensusPolicyError{"app hash mismatch"}
	ErrDataHash                  = &ConsensusPolicyError{"data hash mismatch"}
	ErrLastResultsHash           = &ConsensusPolicyError{"last results hash mismatch"}
	ErrLastBlockID               = &ConsensusPolicyError{"last block ID mismatch"}
	ErrConsensusHash             = &ConsensusPolicyError{"consensus hash mismatch"}
	ErrValidatorsHash            = &ConsensusPolicyError{"validators hash mismatch"}
	ErrNextValidatorsHash        = &ConsensusPolicyError{"next validators hash mismatch"}
	ErrLastCommitVerify          = &ConsensusPolicyError{"last commit verification failed"}
	ErrProposerNotInValidatorSet = &ConsensusPolicyError{"proposer not in validator set"}
	// Distinct from the ErrEvidenceOverflow struct (evidence.go), which carries
	// Max/Got and rides along as the inner %w cause.
	ErrTooMuchEvidence          = &ConsensusPolicyError{"evidence size exceeds limit"}
	ErrLastCommitHash           = &ConsensusPolicyError{"last commit hash mismatch"}
	ErrEvidenceHash             = &ConsensusPolicyError{"evidence hash mismatch"}
	ErrPerEvidenceValidateBasic = &ConsensusPolicyError{"evidence failed ValidateBasic"}
	// x/upgrade BeginBlocker raises this for a not-yet-reached upgrade the binary
	// already handles; swallow-eligible so a replay can run past it.
	ErrUpgradeBeforeTrigger = &ConsensusPolicyError{"binary updated before upgrade trigger"}
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
		ErrUpgradeBeforeTrigger,
	}
}
