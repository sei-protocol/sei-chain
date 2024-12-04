package types

import (
	"fmt"

	paramtypes "github.com/cosmos/cosmos-sdk/x/params/types"
)

const DenomAllowListMaxSize = 2000

// ParamKeyTable ParamTable for tokenfactory module.
func ParamKeyTable() paramtypes.KeyTable {
	return paramtypes.NewKeyTable().RegisterParamSet(&Params{})
}

// DefaultParams default tokenfactory module parameters.
func DefaultParams() Params {
	return Params{
		DenomAllowlistMaxSize: DenomAllowListMaxSize,
	}
}

// Validate validate params.
func (p Params) Validate() error {
	if err := validateDenomAllowListMaxSize(p.DenomAllowlistMaxSize); err != nil {
		return err
	}
	return nil
}

// ParamSetPairs Implements params.ParamSet.
func (p *Params) ParamSetPairs() paramtypes.ParamSetPairs {
	return paramtypes.ParamSetPairs{
		paramtypes.NewParamSetPair(DenomAllowListMaxSizeKey, &p.DenomAllowlistMaxSize, validateDenomAllowListMaxSize),
	}
}

// validateDenomAllowListMaxSize validates a parameter value is within a valid range.
func validateDenomAllowListMaxSize(i interface{}) error {
	val, ok := i.(int32)
	if !ok {
		return fmt.Errorf("invalid parameter type: %T", i)
	}
	if val < 0 {
		return fmt.Errorf("denom allowlist max size must be a non-negative integer")
	}

	return nil
}
