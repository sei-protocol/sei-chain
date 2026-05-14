// Package types — ConsensusPolicy is the build-tag-selected gating surface
// for sei-tendermint's halting validation checks.
//
// # Shape
//
// ConsensusPolicy is a zero-sized struct with a single method:
//
//	func (ConsensusPolicy) ShouldSwallow(kind ErrorKind) bool
//
// Each call site that performs a halting check consults the policy. When
// ShouldSwallow returns true, the site logs the divergence (expected/got),
// increments the sei_unsafe_validation_skipped_total counter, and continues
// past the failure. When it returns false, the original error-return
// behavior is preserved. Sites always run the comparison authentically;
// the policy only governs the failure-handling decision.
//
// # Variant matrix
//
// The struct's method is declared in exactly one of three per-variant files,
// selected by build tag (same-receiver redeclaration is forbidden in Go, so
// each variant is its own compilation unit):
//
//	consensus_policy_default.go              // !mock_block_validation && !mock_chain_validation
//	consensus_policy_mock_block_validation.go // mock_block_validation
//	consensus_policy_mock_chain_validation.go // mock_chain_validation  (M2 deliverable)
//
//	+------------------------+-----------------------------+
//	| build                  | ShouldSwallow behavior      |
//	+------------------------+-----------------------------+
//	| default (production)   | always false                |
//	| mock_block_validation  | true for AppHash, DataHash; |
//	|                        | false otherwise             |
//	| mock_chain_validation  | true for every audit-row    |
//	|                        | swallow-eligible ErrorKind  |
//	+------------------------+-----------------------------+
//
// # Design — single-method collapse
//
// An earlier draft exposed thirteen Swallow*Failure() bool methods on the
// policy (one per swallow-eligible audit row). Review feedback collapsed
// the surface to a single ShouldSwallow(kind) method consulting an internal
// set: O(1) lookup, one method to maintain per variant instead of 13, and
// the policy file's switch on ErrorKind doubles as a registry of which
// failures the variant tolerates.
//
// # Design — no Skip*Validation short-circuit
//
// An earlier shape distinguished "skip" (short-circuit before the
// comparison) from "swallow" (run the comparison, then conditionally
// continue). The current design retains only swallow semantics: every
// check computes its comparison, then the policy decides whether to halt
// on failure. This keeps the build-tag variants on the same code path as
// production (closer to authentic prod behavior) at the cost of
// mock_block_validation losing its Data.Hash(false) compute fast-path —
// an explicitly accepted trade.
//
// # Carve-out — LastResultsHash atomic
//
// One Skip*-style early-return guard is preserved: the
// tmtypes.SkipLastResultsHashValidation atomic.Bool consulted at
// internal/state/validation.go for the LastResultsHash check. That flag
// is set unconditionally by Giga at app init and is load-bearing for
// Giga's production halt-resistance. Migrating Giga to a build-tagged
// ConsensusPolicy variant is its own future workstream. The asymmetry
// is documented inline at the consulting site.
package types

// DefaultConsensusPolicy returns the zero-value policy. Behavior is
// build-tag-dependent: production builds enforce every check;
// mock_block_validation builds swallow AppHash and DataHash mismatches;
// mock_chain_validation (M2) swallows every audit-row swallow-eligible
// failure. See package documentation above.
func DefaultConsensusPolicy() ConsensusPolicy { return ConsensusPolicy{} }

// ErrorKind names a swallow-eligible validation failure. Each constant
// corresponds to a row in the M1.0 audit table (docs/designs/
// mock-chain-validation-m1-audit.md). The string value is the metric label
// emitted on sei_unsafe_validation_skipped_total{kind=...} when the failure
// is swallowed.
type ErrorKind string

const (
	// ErrorKindAppHash — internal/state/validation.go AppHash check.
	// Audit row 1. block.AppHash vs state.AppHash. Forked-boot block-1
	// blocker; the export's AppHash will not match what the new state
	// produces.
	ErrorKindAppHash ErrorKind = "app_hash"

	// ErrorKindDataHash — types/block.go Data.Hash() check in
	// Block.ValidateBasic. Audit row 2. Forked-boot block-1 blocker.
	ErrorKindDataHash ErrorKind = "data_hash"

	// ErrorKindLastResultsHash — internal/state/validation.go
	// LastResultsHash check. Audit row 8. Already gated by the
	// SkipLastResultsHashValidation atomic for Giga; this policy entry
	// allows mock_chain_validation to participate without depending on
	// Giga's runtime flip.
	ErrorKindLastResultsHash ErrorKind = "last_results_hash"

	// ErrorKindLastBlockID — internal/state/validation.go LastBlockID
	// continuity check. Audit row 6. Hash check (BlockID is hash +
	// part-set-header); forked-boot diverges here at block-1.
	ErrorKindLastBlockID ErrorKind = "last_block_id"

	// ErrorKindConsensusHash — internal/state/validation.go
	// ConsensusHash check. Audit row 7. ConsensusParams.Hash mismatch
	// fires when the exported chain had different consensus params than
	// the new genesis.
	ErrorKindConsensusHash ErrorKind = "consensus_hash"

	// ErrorKindValidatorsHash — internal/state/validation.go
	// ValidatorsHash check. Audit row 9. Primary block-1 blocker for
	// forked-boot with a new validator set.
	ErrorKindValidatorsHash ErrorKind = "validators_hash"

	// ErrorKindNextValidatorsHash — internal/state/validation.go
	// NextValidatorsHash check. Audit row 10. Sibling to ValidatorsHash
	// for next-height validator set.
	ErrorKindNextValidatorsHash ErrorKind = "next_validators_hash"

	// ErrorKindLastCommitVerify — internal/state/validation.go
	// state.LastValidators.VerifyCommit. Audit row 12. Load-bearing
	// block-1 blocker for forked-boot — the new LastCommit is signed by
	// the new validators, which will not verify against the exported set.
	ErrorKindLastCommitVerify ErrorKind = "last_commit_verify"

	// ErrorKindProposerNotInValidatorSet — internal/state/validation.go
	// state.Validators.HasAddress(ProposerAddress) check. Audit row 13.
	// Forked-boot with a new validator set: the proposer is in the new
	// set, not the exported one.
	ErrorKindProposerNotInValidatorSet ErrorKind = "proposer_not_in_validator_set"

	// ErrorKindEvidenceOverflow — internal/state/validation.go
	// Evidence.ByteSize > MaxBytes check. Audit row 17. Possible block-1
	// blocker if the export carries evidence near the limit.
	ErrorKindEvidenceOverflow ErrorKind = "evidence_overflow"

	// ErrorKindLastCommitHash — types/block.go LastCommitHash check in
	// Block.ValidateBasic (with legacy-6.4 fallback). Audit row 18.
	// Forked-boot block-1: new LastCommit's hash will not match the
	// export's expected LastCommitHash.
	ErrorKindLastCommitHash ErrorKind = "last_commit_hash"

	// ErrorKindEvidenceHash — types/block.go EvidenceHash check in
	// Block.ValidateBasic. Audit row 19. Hash check.
	ErrorKindEvidenceHash ErrorKind = "evidence_hash"

	// ErrorKindPerEvidenceValidateBasic — types/block.go per-evidence
	// ValidateBasic loop in Block.ValidateBasic. Audit row 20.
	// Cryptographic well-formedness per item; swallowing lets builds
	// keep moving past malformed export-carried evidence.
	ErrorKindPerEvidenceValidateBasic ErrorKind = "per_evidence_validate_basic"
)

// AllSwallowEligibleErrorKinds returns the 13 ErrorKind constants in the
// audit's swallow-eligible set. Useful for tests asserting the variant
// matrix (every test should iterate this list and check ShouldSwallow's
// return for each).
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
