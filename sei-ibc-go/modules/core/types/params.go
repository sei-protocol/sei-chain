package types

import (
	"fmt"

	paramtypes "github.com/sei-protocol/sei-chain/sei-cosmos/x/params/types"
)

var (
	KeyInboundEnabled  = []byte("InboundEnabled")
	KeyOutboundEnabled = []byte("OutboundEnabled")
)

type Params struct {
	InboundEnabled  bool `json:"inbound_enabled" yaml:"inbound_enabled"`
	OutboundEnabled bool `json:"outbound_enabled" yaml:"outbound_enabled"`
}

// ParamKeyTable for the ibc core module params
func ParamKeyTable() paramtypes.KeyTable {
	return paramtypes.NewKeyTable().RegisterParamSet(&Params{})
}

func NewParams(inbound, outbound bool) Params {
	return Params{
		InboundEnabled:  inbound,
		OutboundEnabled: outbound,
	}
}

func DefaultParams() Params {
	return NewParams(true, true)
}

func (p *Params) ParamSetPairs() paramtypes.ParamSetPairs {
	return paramtypes.ParamSetPairs{
		paramtypes.NewParamSetPair(KeyInboundEnabled, &p.InboundEnabled, validateBool),
		paramtypes.NewParamSetPair(KeyOutboundEnabled, &p.OutboundEnabled, validateBool),
	}
}

func (p Params) Validate() error {
	if err := validateBool(p.InboundEnabled); err != nil {
		return fmt.Errorf("inbound_enabled: %w", err)
	}
	if err := validateBool(p.OutboundEnabled); err != nil {
		return fmt.Errorf("outbound_enabled: %w", err)
	}
	return nil
}

func validateBool(i interface{}) error {
	_, ok := i.(bool)
	if !ok {
		return fmt.Errorf("invalid parameter type: %T", i)
	}
	return nil
}
