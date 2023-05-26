package types

import (
	sdk "github.com/cosmos/cosmos-sdk/types"
)

func DefaultFeesParams() *FeesParams {
	return &FeesParams{
		GlobalMinimumGasPrices: sdk.DecCoins{
			sdk.NewDecCoinFromDec(sdk.DefaultBondDenom, sdk.NewDecWithPrec(1, 2)), // 0.01 by default on a chain level
		},
	}
}

// DefaultGenesis returns the default Capability genesis state
func DefaultGenesis() *GenesisState {
	return &GenesisState{
		FeesParams: *DefaultFeesParams(),
	}
}

func (gs GenesisState) Validate() error {
	return gs.FeesParams.Validate()
}
