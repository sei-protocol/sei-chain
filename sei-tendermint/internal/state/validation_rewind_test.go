package state_test

import (
	"errors"
	"testing"

	dbm "github.com/tendermint/tm-db"

	"github.com/sei-protocol/sei-chain/sei-tendermint/internal/eventbus"
	"github.com/sei-protocol/sei-chain/sei-tendermint/internal/proxy"
	sm "github.com/sei-protocol/sei-chain/sei-tendermint/internal/state"
	statefactory "github.com/sei-protocol/sei-chain/sei-tendermint/internal/state/test/factory"
	"github.com/sei-protocol/sei-chain/sei-tendermint/internal/store"
	"github.com/sei-protocol/sei-chain/sei-tendermint/libs/utils/require"
	"github.com/sei-protocol/sei-chain/sei-tendermint/types"
)

func TestValidateBlockRejectsDiscardedBlock(t *testing.T) {
	ctx := t.Context()

	eventBus := eventbus.NewDefault()
	require.NoError(t, eventBus.Start(ctx))

	state, stateDB, privVals := makeState(t, 1, 1)
	proxyApp := proxy.New(&testApp{})
	blockExec := sm.NewBlockExecutor(
		sm.NewStore(stateDB),
		proxyApp,
		makeTxMempool(t, proxyApp),
		sm.EmptyEvidencePool{},
		store.NewBlockStore(dbm.NewMemDB()),
		eventBus,
		types.DefaultConsensusPolicy(),
	)
	lastCommit := &types.Commit{}
	for height := int64(1); height < 3; height++ {
		state, _, lastCommit = makeAndCommitGoodBlock(ctx, t,
			state, height, lastCommit, state.Validators.GetProposer().Address, blockExec, privVals, nil)
	}

	block := statefactory.MakeBlock(state, 3, lastCommit)
	restore := types.ReplaceRewinds([]types.Rewind{{
		ChainID:    state.ChainID,
		SafeHeight: 2,
		Discarded:  []types.DiscardedBlock{{Height: 3, Hash: block.Hash()}},
	}})
	err := blockExec.ValidateBlock(ctx, state, block)
	require.True(t, errors.Is(err, types.ErrDiscardedBlock), "got %v", err)
	restore()

	require.NoError(t, blockExec.ValidateBlock(ctx, state, block))
}
