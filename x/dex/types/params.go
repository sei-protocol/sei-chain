package types

import (
	fmt "fmt"

	sdk "github.com/cosmos/cosmos-sdk/types"
	paramtypes "github.com/cosmos/cosmos-sdk/x/params/types"
	"gopkg.in/yaml.v2"
)

var (
	KeyPriceSnapshotRetention = []byte("PriceSnapshotRetention") // number of epochs to retain price snapshots for
	KeySudoCallGasPrice       = []byte("KeySudoCallGasPrice")    // gas price for sudo calls from endblock
	KeyBeginBlockGasLimit     = []byte("KeyBeginBlockGasLimit")
	KeyEndBlockGasLimit       = []byte("KeyEndBlockGasLimit")
)

const (
	DefaultPriceSnapshotRetention = 24 * 3600  // default to one day
	DefaultBeginBlockGasLimit     = 200000000  // 200M
	DefaultEndBlockGasLimit       = 1000000000 // 1B
)

var DefaultSudoCallGasPrice = sdk.ZeroDec() // 0

var _ paramtypes.ParamSet = (*Params)(nil)

// ParamKeyTable the param key table for launch module
func ParamKeyTable() paramtypes.KeyTable {
	return paramtypes.NewKeyTable().RegisterParamSet(&Params{})
}

// NewParams creates a new Params instance
func NewParams() Params {
	return Params{}
}

// DefaultParams returns a default set of parameters
func DefaultParams() Params {
	return Params{
		PriceSnapshotRetention: DefaultPriceSnapshotRetention,
		SudoCallGasPrice:       DefaultSudoCallGasPrice,
		BeginBlockGasLimit:     DefaultBeginBlockGasLimit,
		EndBlockGasLimit:       DefaultEndBlockGasLimit,
	}
}

// ParamSetPairs get the params.ParamSet
func (p *Params) ParamSetPairs() paramtypes.ParamSetPairs {
	return paramtypes.ParamSetPairs{
		paramtypes.NewParamSetPair(KeyPriceSnapshotRetention, &p.PriceSnapshotRetention, validatePriceSnapshotRetention),
		paramtypes.NewParamSetPair(KeySudoCallGasPrice, &p.SudoCallGasPrice, validateSudoCallGasPrice),
		paramtypes.NewParamSetPair(KeyBeginBlockGasLimit, &p.BeginBlockGasLimit, validateBeginBlockGasLimit),
		paramtypes.NewParamSetPair(KeyEndBlockGasLimit, &p.EndBlockGasLimit, validateEndBlockGasLimit),
	}
}

// Validate validates the set of params
func (p Params) Validate() error {
	return nil
}

// String implements the Stringer interface.
func (p Params) String() string {
	out, _ := yaml.Marshal(p)
	return string(out)
}

func validatePriceSnapshotRetention(i interface{}) error {
	v, ok := i.(uint64)
	if !ok {
		return fmt.Errorf("invalid parameter type: %T", i)
	}

	if v == 0 {
		return fmt.Errorf("price snapshot retention must be a positive integer: %d", v)
	}

	return nil
}

func validateSudoCallGasPrice(i interface{}) error {
	return nil
}

func validateBeginBlockGasLimit(i interface{}) error {
	_, ok := i.(uint64)
	if !ok {
		return fmt.Errorf("invalid parameter type: %T", i)
	}

	return nil
}

func validateEndBlockGasLimit(i interface{}) error {
	_, ok := i.(uint64)
	if !ok {
		return fmt.Errorf("invalid parameter type: %T", i)
	}

	return nil
}
