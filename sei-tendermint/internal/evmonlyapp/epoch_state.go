package evmonlyapp

import (
	"fmt"
	"slices"

	gigatypes "github.com/sei-protocol/sei-chain/sei-db/state_db/giga/types"
	"github.com/sei-protocol/sei-chain/sei-db/state_db/sc/flatkv"
	"github.com/sei-protocol/sei-chain/sei-tendermint/internal/proxy"
	"github.com/sei-protocol/sei-chain/sei-tendermint/libs/utils"
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

// ReadCheckpointEpochRegistryRow returns the epoch_registry row in the FlatKV checkpoint in dir,
// as consensus passed it in EpochState.EpochRegistry, or None if the checkpoint holds no row.
func ReadCheckpointEpochRegistryRow(dir string) (utils.Option[[]byte], error) {
	raw, found, err := flatkv.ReadSnapshotMiscValue(dir, epochStateModule, []byte(epochStateEpochRegistryKey))
	if err != nil {
		return utils.None[[]byte](), fmt.Errorf("read the epoch_registry row: %w", err)
	}
	if !found {
		return utils.None[[]byte](), nil
	}
	return utils.Some(raw), nil
}
