package types

import (
	"errors"
	"fmt"
	"strings"

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

// ParamTable for minting module.
func ParamKeyTable() KeyTable {
	return NewKeyTable(
		NewParamSetPair(ParamStoreKeyFeesParams, &FeesParams{}, validateFeesParams),
		NewParamSetPair(ParamStoreKeyCosmosGasParams, &CosmosGasParams{}, validateCosmosGasParams),
		NewParamSetPair(ParamStoreKeyDisabledMsgTypeURLs, &[]string{}, validateDisabledMsgTypeURLs),
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

// ValidateDisabledMsgTypeURLs returns an error unless every entry of typeURLs has the form of
// a message type URL: a slash followed by the message's protobuf name.
func ValidateDisabledMsgTypeURLs(typeURLs []string) error {
	for _, typeURL := range typeURLs {
		if len(typeURL) < 2 || !strings.HasPrefix(typeURL, "/") {
			return fmt.Errorf("disabled message type URL %q is not a slash followed by a message name", typeURL)
		}
	}
	return nil
}

func validateDisabledMsgTypeURLs(i interface{}) error {
	v, ok := i.([]string)
	if !ok {
		return fmt.Errorf("invalid parameter type: %T", i)
	}
	return ValidateDisabledMsgTypeURLs(v)
}
