package types

import (
	"fmt"

	sdk "github.com/cosmos/cosmos-sdk/types"
	paramtypes "github.com/cosmos/cosmos-sdk/x/params/types"
)

// Parameter store keys.
var (
	KeyWhitelistedTxSenders = []byte("WhitelistedTxSenders")
	KeyEnabled              = []byte("Enabled")
)

// ParamTable for gamm module.
func ParamKeyTable() paramtypes.KeyTable {
	return paramtypes.NewKeyTable().RegisterParamSet(&Params{})
}

func NewParams(whitelistedTxSenders []string, enabled bool) Params {
	return Params{
		WhitelistedTxSenders: whitelistedTxSenders,
		Enabled:              enabled,
	}
}

// default gamm module parameters.
func DefaultParams() Params {
	return Params{
		WhitelistedTxSenders: []string{},
		Enabled:              false,
	}
}

// validate params.
func (p Params) Validate() error {
	if err := validateWhitelistedTxSenders(p.WhitelistedTxSenders); err != nil {
		return err
	}
	if err := validateEnabled(p.Enabled); err != nil {
		return err
	}

	return nil
}

// Implements params.ParamSet.
func (p *Params) ParamSetPairs() paramtypes.ParamSetPairs {
	return paramtypes.ParamSetPairs{
		paramtypes.NewParamSetPair(KeyWhitelistedTxSenders, &p.WhitelistedTxSenders, validateWhitelistedTxSenders),
		paramtypes.NewParamSetPair(KeyEnabled, &p.Enabled, validateEnabled),
	}
}

func validateWhitelistedTxSenders(i interface{}) error {
	v, ok := i.([]string)
	if !ok {
		return fmt.Errorf("invalid parameter type: %T", i)
	}

	for _, acct := range v {
		if _, err := sdk.AccAddressFromBech32(acct); err != nil {
			return fmt.Errorf("invalid whitelisted tx sender: %s", acct)
		}
	}

	return nil
}

func validateEnabled(i interface{}) error {
	_, ok := i.(bool)
	if !ok {
		return fmt.Errorf("invalid parameter type: %T", i)
	}
	return nil
}
