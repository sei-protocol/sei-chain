//go:build mock_block_validation

package types

// ConsensusPolicy is empty in mock_block_validation builds. Each Skip*
// method returns true unconditionally — running this binary IS the bypass.
type ConsensusPolicy struct{}

func (ConsensusPolicy) SkipAppHashValidation() bool  { return true }
func (ConsensusPolicy) SkipDataHashValidation() bool { return true }
