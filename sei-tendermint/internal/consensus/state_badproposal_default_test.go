//go:build !mock_chain_validation && !mock_block_validation

// A proposal carrying a bad AppHash is rejected and prevoted nil only in the
// default build; a mock validation build swallows the AppHash mismatch and
// prevotes for the block, so this assertion is default-build only.
package consensus

import (
	"testing"

	tmconfig "github.com/sei-protocol/sei-chain/sei-tendermint/config"
	"github.com/sei-protocol/sei-chain/sei-tendermint/crypto"
	"github.com/sei-protocol/sei-chain/sei-tendermint/libs/utils"
	"github.com/sei-protocol/sei-chain/sei-tendermint/libs/utils/require"
	tmproto "github.com/sei-protocol/sei-chain/sei-tendermint/proto/tendermint/types"
	"github.com/sei-protocol/sei-chain/sei-tendermint/types"
)

func TestStateBadProposal(t *testing.T) {
	config := configSetup(t)
	chainID := tmconfig.TestLoadGenesis(config).ChainID
	ctx := t.Context()

	cs1, vss := makeState(ctx, t, makeStateArgs{config: config, validators: 2})
	height, round := cs1.roundState.Height(), cs1.roundState.Round()
	vs2 := vss[1]

	partSize := types.BlockPartSizeBytes
	proposalCh := subscribe(ctx, t, cs1.eventBus, types.EventQueryCompleteProposal)
	voteCh := subscribe(ctx, t, cs1.eventBus, types.EventQueryVote)

	propBlock, err := cs1.createProposalBlock(ctx)
	require.NoError(t, err)

	round++
	incrementRound(vss[1:]...)

	stateHash := propBlock.AppHash
	if len(stateHash) == 0 {
		stateHash = make([]byte, 32)
	}
	stateHash[0] = (stateHash[0] + 1) % 255
	propBlock.AppHash = stateHash
	propBlockParts, err := propBlock.MakePartSet(partSize)
	require.NoError(t, err)
	blockID := types.BlockID{Hash: propBlock.Hash(), PartSetHeader: propBlockParts.Header()}
	pubKey, err := vss[1].PrivValidator.GetPubKey(ctx)
	require.NoError(t, err)
	proposal := types.NewProposal(vs2.Height, round, -1, blockID, propBlock.Header.Time, propBlock.GetTxHashes(), propBlock.Header, propBlock.LastCommit, propBlock.Evidence, pubKey.Address())
	p := proposal.ToProto()
	require.NoError(t, vs2.SignProposal(ctx, chainID, p))
	proposal.Signature = utils.OrPanic1(crypto.SigFromBytes(p.Signature))

	err = cs1.SetProposalAndBlock(ctx, proposal, propBlock, propBlockParts, "some peer")
	require.NoError(t, err)

	cs1.startTestRound(ctx, height, round)
	ensureProposal(t, proposalCh, height, round, blockID)
	ensurePrevoteMatch(t, voteCh, height, round, nil)
	cs1.signAddVotes(ctx, t, tmproto.PrevoteType, chainID, blockID, vs2)
	ensurePrevote(t, voteCh, height, round)
	ensurePrecommit(t, voteCh, height, round)
	cs1.validatePrecommit(ctx, t, round, -1, vss[0], nil, nil)
	cs1.signAddVotes(ctx, t, tmproto.PrecommitType, chainID, blockID, vs2)
}
