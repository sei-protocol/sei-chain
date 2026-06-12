//go:build !mock_block_validation && !mock_chain_validation

package types

type ConsensusPolicy struct{}

func (ConsensusPolicy) HandleError(err error) error { return err }
