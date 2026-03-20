package simulation

import (
	"github.com/sei-protocol/sei-chain/sei-cosmos/types/module"

	"github.com/sei-protocol/sei-chain/sei-wasmd/x/wasm/types"
)

// RandomizeGenState generates a random GenesisState for wasm
func RandomizedGenState(simstate *module.SimulationState) {
	params := RandomParams(simstate.Rand)
	wasmGenesis := types.GenesisState{
		Params:    params,
		Codes:     nil,
		Contracts: nil,
		Sequences: []types.Sequence{
			{IDKey: types.KeyLastCodeID, Value: simstate.Rand.Uint64()},
			{IDKey: types.KeyLastInstanceID, Value: simstate.Rand.Uint64()},
		},
		GenMsgs: nil,
	}

	_, err := simstate.Cdc.MarshalAsJSON(&wasmGenesis)
	if err != nil {
		panic(err)
	}

	simstate.GenState[types.ModuleName] = simstate.Cdc.MustMarshalJSON(&wasmGenesis)
}
