//go:build !mock_block_validation

package types

// ConsensusPolicy is empty in production builds. Its methods return
// constant false so the compiler DCEs the bypass branches.
type ConsensusPolicy struct{}

func (ConsensusPolicy) SkipAppHashValidation() bool  { return false }
func (ConsensusPolicy) SkipDataHashValidation() bool { return false }
