package evmonlyapp

import (
	"testing"

	"github.com/sei-protocol/sei-chain/sei-tendermint/internal/proxy"
	"github.com/sei-protocol/sei-chain/sei-tendermint/libs/utils/require"
)

func TestEpochStateMiscChanges(t *testing.T) {
	state := proxy.EpochState{
		EpochRegistry: nil,
		NextEpoch:     []byte("next-epoch"),
	}

	changes := epochStateMiscChanges(state)
	require.Len(t, changes, 2)
	require.Equal(t, epochStateModule, changes[0].Module)
	require.Equal(t, []byte(epochStateEpochRegistryKey), changes[0].Key)
	require.Equal(t, state.EpochRegistry, changes[0].Value)
	require.Equal(t, epochStateModule, changes[1].Module)
	require.Equal(t, []byte(epochStateNextEpochKey), changes[1].Key)
	require.Equal(t, state.NextEpoch, changes[1].Value)
}
