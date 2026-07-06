package state

import (
	abci "github.com/sei-protocol/sei-chain/sei-tendermint/abci/types"
	"github.com/sei-protocol/sei-chain/sei-tendermint/types"
)

// ValidateValidatorUpdates is an alias for validateValidatorUpdates exported
// from execution.go, exclusively and explicitly for testing.
func ValidateValidatorUpdates(abciUpdates []abci.ValidatorUpdate, params types.ValidatorParams) error {
	return validateValidatorUpdates(abciUpdates, params)
}

// ProposerPriorityHashInterval is the interval constant exposed for testing.
const ProposerPriorityHashInterval = proposerPriorityHashInterval

// BuildLastCommitInfo is an alias for buildLastCommitInfo exported for testing
// the mock_chain_validation best-effort path (commit/validator-set size mismatch).
func BuildLastCommitInfo(block *types.Block, store Store, initialHeight int64) abci.CommitInfo {
	return buildLastCommitInfo(block, store, initialHeight)
}
