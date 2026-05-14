//go:build !mock_block_validation && !mock_chain_validation

package types

type ConsensusPolicy struct{}

func (ConsensusPolicy) ShouldSwallow(_ ErrorKind) bool { return false }
