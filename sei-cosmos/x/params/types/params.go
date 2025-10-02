package types

import (
	"errors"
	"fmt"

	sdk "github.com/cosmos/cosmos-sdk/types"
)

var ParamStoreKeyFeesParams = []byte("FeesParams")
var ParamStoreKeyCosmosGasParams = []byte("CosmosGasParams")

func NewFeesParams(minGasPrices sdk.DecCoins) FeesParams {
	return FeesParams{
		GlobalMinimumGasPrices: minGasPrices,
	}
}

// ParamTable for minting module.
func ParamKeyTable() KeyTable {
	return NewKeyTable(
		NewParamSetPair(ParamStoreKeyFeesParams, &FeesParams{}, validateFeesParams),
		NewParamSetPair(ParamStoreKeyCosmosGasParams, &CosmosGasParams{}, validateCosmosGasParams),
	)
}

func (fp *FeesParams) Validate() error {
	for _, fee := range fp.GlobalMinimumGasPrices {
		if err := fee.Validate(); err != nil {
			return err
		}
	}
	return nil
}

func validateFeesParams(i interface{}) error {
	v, ok := i.(FeesParams)
	if !ok {
		return fmt.Errorf("invalid parameter type: %T", i)
	}

	if err := v.Validate(); err != nil {
		return err
	}
	return nil
}

func NewCosmosGasParams(multiplierNumerator uint64, multiplierDenominator uint64) CosmosGasParams {
	return CosmosGasParams{
		CosmosGasMultiplierNumerator:   multiplierNumerator,
		CosmosGasMultiplierDenominator: multiplierDenominator,
	}
}

func (cg *CosmosGasParams) Validate() error {
	if cg.CosmosGasMultiplierNumerator == 0 {
		return errors.New("cosmos gas multiplier numerator can not be 0")
	}

	if cg.CosmosGasMultiplierDenominator == 0 {
		return errors.New("cosmos gas multiplier denominator can not be 0")
	}

	return nil
}

func validateCosmosGasParams(i interface{}) error {
	v, ok := i.(CosmosGasParams)
	if !ok {
		return fmt.Errorf("invalid parameter type: %T", i)
	}

	if err := v.Validate(); err != nil {
		return err
	}
	return nil
}
