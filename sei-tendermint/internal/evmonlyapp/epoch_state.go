package evmonlyapp

import (
	"slices"

	gigatypes "github.com/sei-protocol/sei-chain/sei-db/state_db/giga/types"
	"github.com/sei-protocol/sei-chain/sei-tendermint/internal/proxy"
)

const (
	epochStateModule           = "autobahn"
	epochStateEpochRegistryKey = "epoch_registry"
	epochStateNextEpochKey     = "next_epoch"
)

func epochStateMiscChanges(state proxy.EpochState) []gigatypes.MiscChange {
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
