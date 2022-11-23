package types

import (
	"encoding/json"

	"github.com/cosmos/cosmos-sdk/codec"
)

// NewGenesisState creates a new GenesisState object
func NewGenesisState(
	params Params, rates []ExchangeRateTuple,
	feederDelegations []FeederDelegation, penaltyCounters []PenaltyCounter,
	aggregateExchangeRateVotes []AggregateExchangeRateVote,
	priceSnapshots []PriceSnapshot,
) *GenesisState {
	return &GenesisState{
		Params:                     params,
		ExchangeRates:              rates,
		FeederDelegations:          feederDelegations,
		PenaltyCounters:            penaltyCounters,
		AggregateExchangeRateVotes: aggregateExchangeRateVotes,
		PriceSnapshots:             priceSnapshots,
	}
}

// DefaultGenesisState - default GenesisState used by columbus-2
func DefaultGenesisState() *GenesisState {
	return &GenesisState{
		Params:                     DefaultParams(),
		ExchangeRates:              []ExchangeRateTuple{},
		FeederDelegations:          []FeederDelegation{},
		PenaltyCounters:            []PenaltyCounter{},
		AggregateExchangeRateVotes: []AggregateExchangeRateVote{},
		PriceSnapshots:             PriceSnapshots{},
	}
}

// ValidateGenesis validates the oracle genesis state
func ValidateGenesis(data *GenesisState) error {
	return data.Params.Validate()
}

// GetGenesisStateFromAppState returns x/oracle GenesisState given raw application
// genesis state.
func GetGenesisStateFromAppState(cdc codec.JSONCodec, appState map[string]json.RawMessage) *GenesisState {
	var genesisState GenesisState

	if appState[ModuleName] != nil {
		cdc.MustUnmarshalJSON(appState[ModuleName], &genesisState)
	}

	return &genesisState
}
