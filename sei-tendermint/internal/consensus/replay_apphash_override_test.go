package consensus

import (
	"testing"

	sm "github.com/sei-protocol/sei-chain/sei-tendermint/internal/state"
	"github.com/sei-protocol/sei-chain/sei-tendermint/libs/utils/require"
	"github.com/sei-protocol/sei-chain/sei-tendermint/types"
)

func TestCheckAppHashUsesOverrides(t *testing.T) {
	recorded, replacement := []byte{0xc1}, []byte{0xa1}
	t.Cleanup(types.ReplaceAppHashOverrides([]types.AppHashOverride{
		{ChainID: "replay-override", Height: 7, Recorded: recorded, Replacement: replacement},
	}))

	state := sm.State{ChainID: "replay-override", LastBlockHeight: 7, AppHash: recorded}
	require.NoError(t, checkAppHashEqualsOneFromState(replacement, state))
	state.LastBlockHeight = 8
	require.Error(t, checkAppHashEqualsOneFromState(replacement, state))

	block := &types.Block{Header: types.Header{ChainID: "replay-override", Height: 8, AppHash: recorded}}
	require.NoError(t, checkAppHashEqualsOneFromBlock(replacement, block))
	block.Height = 9
	require.Error(t, checkAppHashEqualsOneFromBlock(replacement, block))
}
