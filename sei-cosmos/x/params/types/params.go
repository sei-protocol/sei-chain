package types

import (
	"fmt"

	sdk "github.com/cosmos/cosmos-sdk/types"
)

var ParamStoreKeyFeesParams = []byte("FeesParams")

func NewFeesParams(minGasPrices sdk.DecCoins) FeesParams {
	return FeesParams{
		GlobalMinimumGasPrices: minGasPrices,
	}
}

// ParamTable for minting module.
func ParamKeyTable() KeyTable {
	return NewKeyTable(
		NewParamSetPair(ParamStoreKeyFeesParams, &FeesParams{}, validateFeesParams),
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
