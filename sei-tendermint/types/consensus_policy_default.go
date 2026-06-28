//go:build !mock_block_validation && !mock_chain_validation

package types

type ConsensusPolicy struct{}

func (ConsensusPolicy) HandleError(err error) error { return err }

// TolerateLastCommitMismatch is false in production: a commit/validator-set size
// divergence is a fatal invariant violation, so buildLastCommitInfo must panic.
func (ConsensusPolicy) TolerateLastCommitMismatch() bool { return false }
