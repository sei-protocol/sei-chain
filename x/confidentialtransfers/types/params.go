package types

import (
	"fmt"

	paramtypes "github.com/cosmos/cosmos-sdk/x/params/types"
)

// DefaultEnableCtModule is the default value for the EnableCtModule flag.
const DefaultEnableCtModule = true

// DefaultRangeProofGasMultiplier is the default value for RangeProofGasMultiplier param.
const DefaultRangeProofGasMultiplier = uint32(10)

// ParamKeyTable ParamTable for confidential transfers module.
func ParamKeyTable() paramtypes.KeyTable {
	return paramtypes.NewKeyTable().RegisterParamSet(&Params{})
}

// DefaultParams default confidential transfers module parameters.
func DefaultParams() Params {
	return Params{
		EnableCtModule:          DefaultEnableCtModule,
		RangeProofGasMultiplier: DefaultRangeProofGasMultiplier,
	}
}

// Validate validate params.
func (p *Params) Validate() error {
	return nil
}

// ParamSetPairs Implements params.ParamSet.
func (p *Params) ParamSetPairs() paramtypes.ParamSetPairs {
	return paramtypes.ParamSetPairs{
		paramtypes.NewParamSetPair(KeyEnableCtModule, &p.EnableCtModule, validateEnableCtModule),
		paramtypes.NewParamSetPair(KeyRangeProofGas, &p.RangeProofGasMultiplier, validateRangeProofGasMultiplier),
	}
}

// Validator for the parameter.
func validateEnableCtModule(i interface{}) error {
	_, ok := i.(bool)
	if !ok {
		return fmt.Errorf("invalid parameter type: %T", i)
	}
	return nil
}

// Validator for the parameter.
func validateRangeProofGasMultiplier(i interface{}) error {
	multiplier, ok := i.(uint32)
	if !ok {
		return fmt.Errorf("invalid parameter type: %T", i)
	}

	if multiplier < 1 {
		return fmt.Errorf("range proof gas multiplier must be greater than 0")
	}
	return nil
}
