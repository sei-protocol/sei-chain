package types

// DefaultConsensusPolicy returns the zero-value policy. Behavior is
// build-tag-dependent: production builds enforce every check; mock_block_validation
// builds bypass every gated check (see consensus_policy_default.go and
// consensus_policy_mock_block_validation.go).
func DefaultConsensusPolicy() ConsensusPolicy { return ConsensusPolicy{} }
