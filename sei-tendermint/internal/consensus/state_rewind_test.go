package consensus

import (
	"testing"
	"time"

	"github.com/sei-protocol/sei-chain/sei-tendermint/internal/proxy"
	"github.com/sei-protocol/sei-chain/sei-tendermint/internal/test/factory"
	"github.com/sei-protocol/sei-chain/sei-tendermint/libs/utils/require"
	"github.com/sei-protocol/sei-chain/sei-tendermint/types"
)

func TestSetProposalRefusesDiscardedBlock(t *testing.T) {
	ctx := t.Context()
	config := configSetup(t)
	state, privVals := makeGenesisState(ctx, t, config, genesisStateArgs{
		Validators: 1,
		Power:      10,
		Params:     factory.ConsensusParams()})
	cs := newStateWithConfig(t, config, state, privVals[0], proxy.New(NewCounterApplication()))
	newBlockCh := subscribe(ctx, t, cs.eventBus, types.EventQueryNewBlock)

	const height = 2
	refused := make(chan error, 1)
	cs.setProposal = func(proposal *types.Proposal, recvTime time.Time) error {
		if proposal.Height == height && len(refused) == 0 {
			restore := types.ReplaceRewinds([]types.Rewind{{
				ChainID:    cs.state.ChainID,
				SafeHeight: height - 1,
				Discarded:  []types.DiscardedBlock{{Height: height, Hash: proposal.BlockID.Hash}},
			}})
			refused <- cs.defaultSetProposal(proposal, recvTime)
			restore()
		}
		return cs.defaultSetProposal(proposal, recvTime)
	}
	cs.startTestRound(ctx, cs.roundState.Height(), cs.roundState.Round())

	ensureNewBlock(t, newBlockCh, height-1)
	ensureNewBlock(t, newBlockCh, height)
	require.ErrorIs(t, <-refused, types.ErrDiscardedBlock)
}
