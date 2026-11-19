package evmonlyapp

import (
	"slices"

	"github.com/sei-protocol/sei-chain/sei-db/bootstrap"
	gigatypes "github.com/sei-protocol/sei-chain/sei-db/state_db/giga/types"
	"github.com/sei-protocol/sei-chain/sei-tendermint/internal/proxy"
)

const epochStateEpochCommitteeKey = "epoch_committee"

// epochStateMiscChanges is the FlatKV write for the epoch state a block carries:
// the persisted registry and the current committee, each as one misc change.
func epochStateMiscChanges(state proxy.EpochState) []gigatypes.MiscChange {
	return []gigatypes.MiscChange{
		{
			Module: bootstrap.EpochStateModule,
			Key:    []byte(bootstrap.EpochRegistryKey),
			Value:  slices.Clone(state.EpochRegistry),
		},
		{
			Module: bootstrap.EpochStateModule,
			Key:    []byte(epochStateEpochCommitteeKey),
			Value:  slices.Clone(state.CurrentCommittee),
		},
	}
}
