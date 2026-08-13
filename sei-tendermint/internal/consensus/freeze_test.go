package consensus

import (
	"testing"

	sm "github.com/sei-protocol/sei-chain/sei-tendermint/internal/state"
	"github.com/stretchr/testify/require"
)

func TestSetFreezeHeight(t *testing.T) {
	state := sm.State{InitialHeight: 1, LastBlockHeight: 9}
	cs := &State{state: state}

	cs.SetFreezeHeight(10)

	require.True(t, cs.frozen.Load())
	require.Equal(t, int64(10), nextHeightForState(state))
}

func TestSetFreezeHeightDisabled(t *testing.T) {
	cs := &State{state: sm.State{InitialHeight: 1, LastBlockHeight: 9}}

	cs.SetFreezeHeight(0)

	require.False(t, cs.frozen.Load())
}
