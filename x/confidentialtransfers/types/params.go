package types

import (
	paramtypes "github.com/cosmos/cosmos-sdk/x/params/types"
)

// ParamKeyTable ParamTable for confidential transfers module.
func ParamKeyTable() paramtypes.KeyTable {
	return paramtypes.NewKeyTable().RegisterParamSet(&Params{})
}

// DefaultParams default confidential transfers module parameters.
func DefaultParams() Params {
	return Params{}
}

// Validate validate params.
func (p *Params) Validate() error {
	return nil
}

// ParamSetPairs Implements params.ParamSet.
func (p *Params) ParamSetPairs() paramtypes.ParamSetPairs {
	return paramtypes.ParamSetPairs{}
}
