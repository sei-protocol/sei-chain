package evmonlyapp

import (
	gigatypes "github.com/sei-protocol/sei-chain/sei-db/state_db/giga/types"
	"github.com/sei-protocol/sei-chain/sei-tendermint/internal/proxy"
)

const (
	consensusStateModule           = "autobahn"
	consensusStateEpochRegistryKey = "epoch_registry"
	consensusStateNextEpochKey     = "next_epoch"
)

func consensusStateMiscChanges(state proxy.ConsensusState) []gigatypes.MiscChange {
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
