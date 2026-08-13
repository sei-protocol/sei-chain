package blocksync

import (
	"testing"

	sm "github.com/sei-protocol/sei-chain/sei-tendermint/internal/state"
	"github.com/stretchr/testify/require"
)

func TestShouldFreeze(t *testing.T) {
	state := sm.State{InitialHeight: 1, LastBlockHeight: 9}

	require.True(t, (&Reactor{freezeHeight: 10}).shouldFreeze(state))
	require.False(t, (&Reactor{freezeHeight: 11}).shouldFreeze(state))
	require.False(t, (&Reactor{}).shouldFreeze(state))
}
