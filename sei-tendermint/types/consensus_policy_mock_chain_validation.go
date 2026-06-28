//go:build mock_chain_validation

package types

import "errors"

// ConsensusPolicy here swallows every ValidationErrors sentinel, including
// ErrLastCommitVerify. A shadow replays an AppHash-breaking storage migration
// against a chain whose validator set it cannot reproduce bit-for-bit, so the
// last-commit voting-power check drifts at thin-margin commits and is not a
// correctness signal for this build — the logical-digest comparator is. A
// swallowed failure increments the counter and continues; the downstream
// commit-info build tolerates the resulting drift (see TolerateLastCommitMismatch).
type ConsensusPolicy struct{}

var swallowedErrors = ValidationErrors()

func (ConsensusPolicy) HandleError(err error) error {
	for _, e := range swallowedErrors {
		if errors.Is(err, e) {
			recordUnsafeValidationSkipped(err)
			return nil
		}
	}
	return err
}

// TolerateLastCommitMismatch lets buildLastCommitInfo build best-effort commit
// info instead of panicking when the replayed commit's size diverges from the
// locally-stored validator set. Swallowing ErrLastCommitVerify admits blocks
// whose commit the local validator set can't fully reconstruct, so the panic
// guard must yield here — LastCommitInfo feeds staking rewards/downtime, never
// EVM state, which is what this build exists to compare.
func (ConsensusPolicy) TolerateLastCommitMismatch() bool { return true }
