package types

import (
	fmt "fmt"

	paramtypes "github.com/cosmos/cosmos-sdk/x/params/types"
	"gopkg.in/yaml.v2"
)

var KeyPriceSnapshotRetention = []byte("PriceSnapshotRetention") // number of epochs to retain price snapshots for

const (
	DefaultPriceSnapshotRetention = 1440 // default epoch interval is one minute so this results in a one-day retention
)

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
	}
}

// ParamSetPairs get the params.ParamSet
func (p *Params) ParamSetPairs() paramtypes.ParamSetPairs {
	return paramtypes.ParamSetPairs{
		paramtypes.NewParamSetPair(KeyPriceSnapshotRetention, &p.PriceSnapshotRetention, validatePriceSnapshotRetention),
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
