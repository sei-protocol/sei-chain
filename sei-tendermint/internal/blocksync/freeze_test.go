package blocksync

import (
	"testing"

	"github.com/stretchr/testify/require"
	sm "github.com/tendermint/tendermint/internal/state"
)

func TestShouldFreeze(t *testing.T) {
	state := sm.State{InitialHeight: 1, LastBlockHeight: 9}

	require.True(t, (&Reactor{freezeHeight: 10}).shouldFreeze(state))
	require.False(t, (&Reactor{freezeHeight: 11}).shouldFreeze(state))
	require.False(t, (&Reactor{}).shouldFreeze(state))
}
