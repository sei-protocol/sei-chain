package evmonlyapp

import (
	"testing"

	"github.com/sei-protocol/sei-chain/sei-tendermint/internal/proxy"
	"github.com/sei-protocol/sei-chain/sei-tendermint/libs/utils/require"
)

func TestConsensusStateMiscChanges(t *testing.T) {
	state := proxy.ConsensusState{
		EpochRegistry: nil,
		NextEpoch:     []byte("next-epoch"),
	}

	changes := consensusStateMiscChanges(state)
	require.Len(t, changes, 2)
	require.Equal(t, consensusStateModule, changes[0].Module)
	require.Equal(t, []byte(consensusStateEpochRegistryKey), changes[0].Key)
	require.Equal(t, state.EpochRegistry, changes[0].Value)
	require.Equal(t, consensusStateModule, changes[1].Module)
	require.Equal(t, []byte(consensusStateNextEpochKey), changes[1].Key)
	require.Equal(t, state.NextEpoch, changes[1].Value)
}
