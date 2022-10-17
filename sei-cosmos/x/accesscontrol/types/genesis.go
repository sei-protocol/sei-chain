package types

import (
	"encoding/json"

	"github.com/cosmos/cosmos-sdk/codec"
	acltypes "github.com/cosmos/cosmos-sdk/types/accesscontrol"
)

// NewGenesisState creates a new GenesisState object
func NewGenesisState(params Params, messageDependencyMapping []acltypes.MessageDependencyMapping, wasmDependencyMappings []acltypes.WasmDependencyMapping) *GenesisState {
	return &GenesisState{
		Params:                   params,
		MessageDependencyMapping: messageDependencyMapping,
		WasmDependencyMappings:   wasmDependencyMappings,
	}
}

// DefaultGenesisState - default GenesisState used by columbus-2
func DefaultGenesisState() *GenesisState {
	return &GenesisState{
		Params:                   DefaultParams(),
		MessageDependencyMapping: DefaultMessageDependencyMapping(),
		WasmDependencyMappings:   DefaultWasmDependencyMappings(),
	}
}

// ValidateGenesis validates the oracle genesis state
func ValidateGenesis(data GenesisState) error {
	for _, mapping := range data.MessageDependencyMapping {
		err := ValidateMessageDependencyMapping(mapping)
		if err != nil {
			return err
		}
	}
	for _, mapping := range data.WasmDependencyMappings {
		err := ValidateWasmDependencyMapping(mapping)
		if err != nil {
			return err
		}
	}
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
