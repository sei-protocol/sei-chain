package evmonlyapp

import (
	"testing"

	"github.com/sei-protocol/sei-chain/sei-db/bootstrap"
	"github.com/sei-protocol/sei-chain/sei-tendermint/internal/proxy"
	"github.com/sei-protocol/sei-chain/sei-tendermint/libs/utils/require"
)

// TestEpochStateMiscChanges pins the two FlatKV writes for an epoch state.
// Dropping a clone lets the caller's later overwrite show up in the change.
func TestEpochStateMiscChanges(t *testing.T) {
	registry := []byte("registry")
	committee := []byte("committee")
	state := proxy.EpochState{EpochRegistry: registry, CurrentCommittee: committee}

	changes := epochStateMiscChanges(state)
	require.Len(t, changes, 2)
	require.Equal(t, bootstrap.EpochStateModule, changes[0].Module)
	require.Equal(t, []byte(bootstrap.EpochRegistryKey), changes[0].Key)
	require.Equal(t, registry, changes[0].Value)
	require.Equal(t, bootstrap.EpochStateModule, changes[1].Module)
	require.Equal(t, []byte(epochStateEpochCommitteeKey), changes[1].Key)
	require.Equal(t, committee, changes[1].Value)

	copy(registry, "XXXXXXXX")
	copy(committee, "YYYYYYYYY")
	require.Equal(t, []byte("registry"), changes[0].Value)
	require.Equal(t, []byte("committee"), changes[1].Value)
}
