package types

import (
	"errors"
	fmt "fmt"

	sdk "github.com/cosmos/cosmos-sdk/types"
	paramtypes "github.com/cosmos/cosmos-sdk/x/params/types"
	"gopkg.in/yaml.v2"
)

var (
	KeyBaseDenom     = []byte("KeyBaseDenom")
	KeyGasMultiplier = []byte("KeyGasMultiplier")
	KeyChainConfig   = []byte("KeyChainConfig")
)

const (
	DefaultBaseDenom = "usei"
)

var DefaultGasMultiplier = sdk.NewDecWithPrec(1, 1)

var _ paramtypes.ParamSet = (*Params)(nil)

func ParamKeyTable() paramtypes.KeyTable {
	return paramtypes.NewKeyTable().RegisterParamSet(&Params{})
}

func DefaultParams() Params {
	return Params{
		BaseDenom:     DefaultBaseDenom,
		GasMultiplier: DefaultGasMultiplier,
		ChainConfig:   DefaultChainConfig(),
	}
}

func (p *Params) ParamSetPairs() paramtypes.ParamSetPairs {
	return paramtypes.ParamSetPairs{
		paramtypes.NewParamSetPair(KeyBaseDenom, &p.BaseDenom, validateBaseDenom),
		paramtypes.NewParamSetPair(KeyGasMultiplier, &p.GasMultiplier, validateGasMultiplier),
		paramtypes.NewParamSetPair(KeyChainConfig, &p.ChainConfig, validateChainConfig),
	}
}

func (p Params) Validate() error {
	if err := validateBaseDenom(p.BaseDenom); err != nil {
		return err
	}
	if err := validateGasMultiplier(p.GasMultiplier); err != nil {
		return err
	}
	if err := validateChainConfig(p.ChainConfig); err != nil {
		return err
	}
	return nil
}

func (p Params) String() string {
	out, _ := yaml.Marshal(p)
	return string(out)
}

func validateBaseDenom(i interface{}) error {
	v, ok := i.(string)
	if !ok {
		return fmt.Errorf("invalid parameter type: %T", i)
	}

	if v == "" {
		return errors.New("empty base denom")
	}

	return nil
}

func validateGasMultiplier(i interface{}) error {
	v, ok := i.(sdk.Dec)
	if !ok {
		return fmt.Errorf("invalid parameter type: %T", i)
	}

	if !v.IsPositive() {
		return fmt.Errorf("gas multiplier: %d", v)
	}

	return nil
}

func validateChainConfig(i interface{}) error {
	v, ok := i.(ChainConfig)
	if !ok {
		return fmt.Errorf("invalid parameter type: %T", i)
	}

	return v.Validate()
}
