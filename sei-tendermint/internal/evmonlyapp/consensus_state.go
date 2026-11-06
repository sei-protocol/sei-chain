package evmonlyapp

import (
	gigatypes "github.com/sei-protocol/sei-chain/sei-db/state_db/giga/types"
	abci "github.com/sei-protocol/sei-chain/sei-tendermint/abci/types"
)

const (
	consensusStateModule           = "autobahn"
	consensusStateEpochRegistryKey = "epoch_registry"
	consensusStateNextEpochKey     = "next_epoch"
)

func consensusStateMiscChanges(state abci.ConsensusState) []gigatypes.MiscChange {
	return []gigatypes.MiscChange{
		consensusStateMiscChange(consensusStateEpochRegistryKey, state.EpochRegistry),
		consensusStateMiscChange(consensusStateNextEpochKey, state.NextEpoch),
	}
}

func consensusStateMiscChange(key string, value []byte) gigatypes.MiscChange {
	return gigatypes.MiscChange{
		Module: consensusStateModule,
		Key:    []byte(key),
		Value:  value,
	}
}
