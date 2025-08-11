package state

import (
	abci "github.com/sei-protocol/sei-chain/tendermint/abci/types"
	"github.com/sei-protocol/sei-chain/tendermint/types"
)

// ValidateValidatorUpdates is an alias for validateValidatorUpdates exported
// from execution.go, exclusively and explicitly for testing.
func ValidateValidatorUpdates(abciUpdates []abci.ValidatorUpdate, params types.ValidatorParams) error {
	return validateValidatorUpdates(abciUpdates, params)
}
