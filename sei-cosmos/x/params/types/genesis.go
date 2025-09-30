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

func DefaultCosmosGasParams() *CosmosGasParams {
	return &CosmosGasParams{
		CosmosGasMultiplierNumerator:   1,
		CosmosGasMultiplierDenominator: 1,
	}
}

// DefaultGenesis returns the default Capability genesis state
func DefaultGenesis() *GenesisState {
	return &GenesisState{
		FeesParams:      *DefaultFeesParams(),
		CosmosGasParams: *DefaultCosmosGasParams(),
	}
}

func NewGenesisState(feesParams FeesParams, cosmosGasParams CosmosGasParams) *GenesisState {
	return &GenesisState{
		FeesParams:      feesParams,
		CosmosGasParams: cosmosGasParams,
	}
}

func (gs GenesisState) Validate() error {
	if err := gs.CosmosGasParams.Validate(); err != nil {
		return err
	}
	return gs.FeesParams.Validate()
}
