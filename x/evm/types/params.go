package types

import (
	"errors"
	fmt "fmt"

	sdk "github.com/cosmos/cosmos-sdk/types"
	paramtypes "github.com/cosmos/cosmos-sdk/x/params/types"
	"gopkg.in/yaml.v2"
)

var (
	KeyBaseDenom          = []byte("KeyBaseDenom")
	KeyPriorityNormalizer = []byte("KeyPriorityNormalizer")
	KeyBaseFeePerGas      = []byte("KeyBaseFeePerGas")
	KeyMinFeePerGas       = []byte("KeyMinFeePerGas")
	KeyChainConfig        = []byte("KeyChainConfig")
	KeyChainID            = []byte("KeyChainID")
)

const (
	DefaultBaseDenom = "usei"
)

var DefaultPriorityNormalizer = sdk.NewDec(1)

// DefaultBaseFeePerGas determines how much usei per gas spent is
// burnt rather than go to validators (similar to base fee on
// Ethereum).
var DefaultBaseFeePerGas = sdk.NewDec(0)
var DefaultMinFeePerGas = sdk.NewDec(1)
var DefaultChainID = sdk.NewInt(713715)

var _ paramtypes.ParamSet = (*Params)(nil)

func ParamKeyTable() paramtypes.KeyTable {
	return paramtypes.NewKeyTable().RegisterParamSet(&Params{})
}

func DefaultParams() Params {
	return Params{
		BaseDenom:          DefaultBaseDenom,
		PriorityNormalizer: DefaultPriorityNormalizer,
		BaseFeePerGas:      DefaultBaseFeePerGas,
		MinimumFeePerGas:   DefaultMinFeePerGas,
		ChainConfig:        DefaultChainConfig(),
		ChainId:            DefaultChainID,
	}
}

func (p *Params) ParamSetPairs() paramtypes.ParamSetPairs {
	return paramtypes.ParamSetPairs{
		paramtypes.NewParamSetPair(KeyBaseDenom, &p.BaseDenom, validateBaseDenom),
		paramtypes.NewParamSetPair(KeyPriorityNormalizer, &p.PriorityNormalizer, validatePriorityNormalizer),
		paramtypes.NewParamSetPair(KeyBaseFeePerGas, &p.BaseFeePerGas, validateBaseFeePerGas),
		paramtypes.NewParamSetPair(KeyMinFeePerGas, &p.MinimumFeePerGas, validateMinFeePerGas),
		paramtypes.NewParamSetPair(KeyChainConfig, &p.ChainConfig, validateChainConfig),
		paramtypes.NewParamSetPair(KeyChainID, &p.ChainId, validateChainID),
	}
}

func (p Params) Validate() error {
	if err := validateBaseDenom(p.BaseDenom); err != nil {
		return err
	}
	if err := validatePriorityNormalizer(p.PriorityNormalizer); err != nil {
		return err
	}
	if err := validateBaseFeePerGas(p.BaseFeePerGas); err != nil {
		return err
	}
	if err := validateMinFeePerGas(p.MinimumFeePerGas); err != nil {
		return err
	}
	if p.MinimumFeePerGas.LT(p.BaseFeePerGas) {
		return errors.New("minimum fee cannot be lower than base fee")
	}
	if err := validateChainID(p.ChainId); err != nil {
		return err
	}
	return validateChainConfig(p.ChainConfig)
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

func validatePriorityNormalizer(i interface{}) error {
	v, ok := i.(sdk.Dec)
	if !ok {
		return fmt.Errorf("invalid parameter type: %T", i)
	}

	if !v.IsPositive() {
		return fmt.Errorf("nonpositive priority normalizer: %d", v)
	}

	return nil
}

func validateBaseFeePerGas(i interface{}) error {
	v, ok := i.(sdk.Dec)
	if !ok {
		return fmt.Errorf("invalid parameter type: %T", i)
	}

	if v.IsNegative() {
		return fmt.Errorf("negative base fee per gas: %d", v)
	}

	return nil
}

func validateMinFeePerGas(i interface{}) error {
	v, ok := i.(sdk.Dec)
	if !ok {
		return fmt.Errorf("invalid parameter type: %T", i)
	}

	if v.IsNegative() {
		return fmt.Errorf("negative min fee per gas: %d", v)
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

func validateChainID(i interface{}) error {
	v, ok := i.(sdk.Int)
	if !ok {
		return fmt.Errorf("invalid parameter type: %T", i)
	}

	if v.IsNegative() {
		return fmt.Errorf("negative min fee per gas: %d", v)
	}

	return nil
}
