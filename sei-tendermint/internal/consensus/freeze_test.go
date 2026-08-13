package consensus

import (
	"testing"

	"github.com/stretchr/testify/require"
	sm "github.com/tendermint/tendermint/internal/state"
	"github.com/tendermint/tendermint/libs/log"
)

func TestSetFreezeHeight(t *testing.T) {
	state := sm.State{InitialHeight: 1, LastBlockHeight: 9}
	cs := &State{logger: log.NewNopLogger(), state: state}

	cs.SetFreezeHeight(10)

	require.True(t, cs.frozen.Load())
	require.Equal(t, int64(10), nextHeightForState(state))
}

func TestSetFreezeHeightDisabled(t *testing.T) {
	cs := &State{logger: log.NewNopLogger(), state: sm.State{InitialHeight: 1, LastBlockHeight: 9}}

	cs.SetFreezeHeight(0)

	require.False(t, cs.frozen.Load())
}
