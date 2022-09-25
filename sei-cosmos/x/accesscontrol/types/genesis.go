package types

import (
	"encoding/json"

	"github.com/cosmos/cosmos-sdk/codec"
)

func DefaultMessageDependencyMapping() []MessageDependencyMapping {
	return []MessageDependencyMapping{
		{
			MessageKey: "",
			AccessOps: []AccessOperation{
				{AccessType: AccessType_UNKNOWN, ResourceType: ResourceType_ANY, IdentifierTemplate: "*"},
			},
		},
	}
}

// NewGenesisState creates a new GenesisState object
func NewGenesisState(params Params, messageDependencyMapping []MessageDependencyMapping) *GenesisState {
	return &GenesisState{
		Params:                   params,
		MessageDependencyMapping: messageDependencyMapping,
	}
}

// DefaultGenesisState - default GenesisState used by columbus-2
func DefaultGenesisState() *GenesisState {
	return &GenesisState{
		Params:                   DefaultParams(),
		MessageDependencyMapping: DefaultMessageDependencyMapping(),
	}
}

// ValidateGenesis validates the oracle genesis state
func ValidateGenesis(data GenesisState) error {
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
