package types

import (
	"errors"
	"fmt"
	"slices"

	codectypes "github.com/sei-protocol/sei-chain/sei-cosmos/codec/types"
	sdk "github.com/sei-protocol/sei-chain/sei-cosmos/types"
)

var ParamStoreKeyFeesParams = []byte("FeesParams")
var ParamStoreKeyCosmosGasParams = []byte("CosmosGasParams")
var ParamStoreKeyDisabledMsgTypeURLs = []byte("DisabledMsgTypeURLs")

func NewFeesParams(minGasPrices sdk.DecCoins) FeesParams {
	return FeesParams{
		GlobalMinimumGasPrices: minGasPrices,
	}
}

// ParamKeyTable returns the key table of the params module's own subspace. The disabled
// message type URLs it accepts are those of the messages registered in registry.
func ParamKeyTable(registry codectypes.InterfaceRegistry) KeyTable {
	return NewKeyTable(
		NewParamSetPair(ParamStoreKeyFeesParams, &FeesParams{}, validateFeesParams),
		NewParamSetPair(ParamStoreKeyCosmosGasParams, &CosmosGasParams{}, validateCosmosGasParams),
		NewParamSetPair(ParamStoreKeyDisabledMsgTypeURLs, &[]string{}, disabledMsgTypeURLsValidator(registry)),
	)
}

func (fp *FeesParams) Validate() error {
	for _, fee := range fp.GlobalMinimumGasPrices {
		if err := fee.Validate(); err != nil {
			return err
		}
	}
	return nil
}

func validateFeesParams(i interface{}) error {
	v, ok := i.(FeesParams)
	if !ok {
		return fmt.Errorf("invalid parameter type: %T", i)
	}

	if err := v.Validate(); err != nil {
		return err
	}
	return nil
}

func NewCosmosGasParams(multiplierNumerator uint64, multiplierDenominator uint64) CosmosGasParams {
	return CosmosGasParams{
		CosmosGasMultiplierNumerator:   multiplierNumerator,
		CosmosGasMultiplierDenominator: multiplierDenominator,
	}
}

func (cg *CosmosGasParams) Validate() error {
	if cg.CosmosGasMultiplierNumerator == 0 {
		return errors.New("cosmos gas multiplier numerator can not be 0")
	}

	if cg.CosmosGasMultiplierDenominator == 0 {
		return errors.New("cosmos gas multiplier denominator can not be 0")
	}

	return nil
}

func validateCosmosGasParams(i interface{}) error {
	v, ok := i.(CosmosGasParams)
	if !ok {
		return fmt.Errorf("invalid parameter type: %T", i)
	}

	if err := v.Validate(); err != nil {
		return err
	}
	return nil
}

// disabledMsgTypeURLsValidator returns the validator of the disabled message type URLs, which
// refuses a list unless every entry is the type URL of a message registered in registry.
func disabledMsgTypeURLsValidator(registry codectypes.InterfaceRegistry) ValueValidatorFn {
	return func(i interface{}) error {
		typeURLs, ok := i.([]string)
		if !ok {
			return fmt.Errorf("invalid parameter type: %T", i)
		}
		msgTypeURLs := registry.ListImplementations(sdk.MsgInterfaceProtoName)
		for _, typeURL := range typeURLs {
			if !slices.Contains(msgTypeURLs, typeURL) {
				return fmt.Errorf("disabled message type URL %q is not the type URL of a registered message", typeURL)
			}
		}
		return nil
	}
}
