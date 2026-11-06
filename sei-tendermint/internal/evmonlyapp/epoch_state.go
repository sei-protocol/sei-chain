package evmonlyapp

import (
	"slices"

	gigatypes "github.com/sei-protocol/sei-chain/sei-db/state_db/giga/types"
	abci "github.com/sei-protocol/sei-chain/sei-tendermint/abci/types"
)

const (
	epochStateModule           = "autobahn"
	epochStateEpochRegistryKey = "epoch_registry"
	epochStateNextEpochKey     = "next_epoch"
)

func epochStateMiscChanges(state abci.EpochState) []gigatypes.MiscChange {
	return []gigatypes.MiscChange{
		epochStateMiscChange(epochStateEpochRegistryKey, state.EpochRegistry),
		epochStateMiscChange(epochStateNextEpochKey, state.NextEpoch),
	}
}

func epochStateMiscChange(key string, value []byte) gigatypes.MiscChange {
	return gigatypes.MiscChange{
		Module: epochStateModule,
		Key:    []byte(key),
		Value:  slices.Clone(value),
	}
}
