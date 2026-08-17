package consensus

import (
	"path/filepath"
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

func TestFrozenStateDoesNotWriteNewStepToWAL(t *testing.T) {
	wal, err := OpenWAL(filepath.Join(t.TempDir(), "wal"))
	require.NoError(t, err)
	defer wal.Close()
	cs := &State{wal: wal}

	cs.newStep()
	_, walMessages, err := wal.ReadLastHeightMsgs()
	require.NoError(t, err)
	require.Len(t, walMessages, 1)

	cs.frozen.Store(true)
	cs.newStep()
	_, walMessages, err = wal.ReadLastHeightMsgs()
	require.NoError(t, err)
	require.Len(t, walMessages, 1)
}
