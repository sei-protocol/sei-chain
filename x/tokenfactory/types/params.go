package types

import (
	"fmt"

	paramtypes "github.com/cosmos/cosmos-sdk/x/params/types"
)

// DefaultDenomAllowListMaxSize default denom allowlist max size and can be overridden by governance proposal.
const DefaultDenomAllowListMaxSize = 2000

// ParamKeyTable ParamTable for tokenfactory module.
func ParamKeyTable() paramtypes.KeyTable {
	return paramtypes.NewKeyTable().RegisterParamSet(&Params{})
}

// DefaultParams default tokenfactory module parameters.
func DefaultParams() Params {
	return Params{
		DenomAllowlistMaxSize: DefaultDenomAllowListMaxSize,
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
	_, ok := i.(uint32)
	if !ok {
		return fmt.Errorf("invalid parameter type: %T", i)
	}
	return nil
}
