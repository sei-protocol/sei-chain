package consensus

import (
	"testing"

	"github.com/gogo/protobuf/proto"

	tmconfig "github.com/sei-protocol/sei-chain/sei-tendermint/config"
	"github.com/sei-protocol/sei-chain/sei-tendermint/crypto"
	tmtime "github.com/sei-protocol/sei-chain/sei-tendermint/libs/time"
	"github.com/sei-protocol/sei-chain/sei-tendermint/libs/utils"
	"github.com/sei-protocol/sei-chain/sei-tendermint/libs/utils/require"
	"github.com/sei-protocol/sei-chain/sei-tendermint/types"
)

func nonCanonicalPartSet(t *testing.T, block *types.Block) *types.PartSet {
	t.Helper()
	pbb, err := block.ToProto()
	require.NoError(t, err)
	bz, err := proto.Marshal(pbb)
	require.NoError(t, err)
	// Unknown field 999, length-delimited "junk".
	nonCanonical := append(append([]byte{}, bz...), 0xba, 0x3e, 0x04, 'j', 'u', 'n', 'k')
	parts := types.NewPartSetFromData(nonCanonical, types.BlockPartSizeBytes)
	canonical, err := block.MakePartSet(types.BlockPartSizeBytes)
	require.NoError(t, err)
	require.False(t, parts.Header().Equals(canonical.Header()))
	return parts
}

func TestRejectNonCanonicalProposalBlockParts(t *testing.T) {
	config := configSetup(t)
	chainID := tmconfig.TestLoadGenesis(config).ChainID
	ctx := t.Context()

	cs1, vss := makeState(ctx, t, makeStateArgs{config: config, validators: 2})
	height, round := cs1.roundState.Height(), cs1.roundState.Round()
	round++
	incrementRound(vss[1:]...)

	propBlock, err := cs1.createProposalBlock(ctx)
	require.NoError(t, err)

	nonCanonicalParts := nonCanonicalPartSet(t, propBlock)
	blockID := types.BlockID{Hash: propBlock.Hash(), PartSetHeader: nonCanonicalParts.Header()}
	pubKey, err := vss[1].PrivValidator.GetPubKey(ctx)
	require.NoError(t, err)
	proposal := types.NewProposal(
		height, round, -1, blockID, propBlock.Time,
		propBlock.GetTxHashes(), propBlock.Header, propBlock.LastCommit, propBlock.Evidence, pubKey.Address(),
	)
	p := proposal.ToProto()
	require.NoError(t, vss[1].SignProposal(ctx, chainID, p))
	proposal.Signature = utils.OrPanic1(crypto.SigFromBytes(p.Signature))

	cs1.startTestRound(ctx, height, round)
	peerID, err := types.NewNodeID("AAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA")
	require.NoError(t, err)

	// Drive consensus synchronously so rejection is visible on round state.
	cs1.handleMsg(ctx, msgInfo{&ProposalMessage{proposal}, peerID, tmtime.Now()}, false)
	for i := 0; i < int(nonCanonicalParts.Total()); i++ {
		cs1.handleMsg(ctx, msgInfo{
			&BlockPartMessage{Height: height, Round: round, Part: nonCanonicalParts.GetPart(i)},
			peerID,
			tmtime.Now(),
		}, false)
	}

	rs := cs1.GetRoundState()
	require.NotNil(t, rs.Proposal, "proposal should still be accepted")
	require.Nil(t, rs.ProposalBlock, "proposal block must not be accepted from non-canonical parts")
}

// Commit/maj23 catch-up can retarget ProposalBlockParts to a certificate
// PartSetHeader while leaving the original Proposal in place. Assembling
// canonical parts for that certificate must succeed even when the proposal's
// BlockID.PartSetHeader differs.
func TestAcceptCanonicalPartsWhenProposalPartSetHeaderDiffers(t *testing.T) {
	config := configSetup(t)
	chainID := tmconfig.TestLoadGenesis(config).ChainID
	ctx := t.Context()

	cs1, vss := makeState(ctx, t, makeStateArgs{config: config, validators: 2})
	height, round := cs1.roundState.Height(), cs1.roundState.Round()
	round++
	incrementRound(vss[1:]...)

	propBlock, err := cs1.createProposalBlock(ctx)
	require.NoError(t, err)
	canonicalParts, err := propBlock.MakePartSet(types.BlockPartSizeBytes)
	require.NoError(t, err)

	staleHeader := nonCanonicalPartSet(t, propBlock).Header()
	require.False(t, staleHeader.Equals(canonicalParts.Header()))

	// Proposal still claims the stale PartSetHeader (as after a mismatched earlier propose).
	blockID := types.BlockID{Hash: propBlock.Hash(), PartSetHeader: staleHeader}
	pubKey, err := vss[1].PrivValidator.GetPubKey(ctx)
	require.NoError(t, err)
	proposal := types.NewProposal(
		height, round, -1, blockID, propBlock.Time,
		propBlock.GetTxHashes(), propBlock.Header, propBlock.LastCommit, propBlock.Evidence, pubKey.Address(),
	)
	p := proposal.ToProto()
	require.NoError(t, vss[1].SignProposal(ctx, chainID, p))
	proposal.Signature = utils.OrPanic1(crypto.SigFromBytes(p.Signature))

	cs1.startTestRound(ctx, height, round)
	peerID, err := types.NewNodeID("AAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA")
	require.NoError(t, err)

	cs1.handleMsg(ctx, msgInfo{&ProposalMessage{proposal}, peerID, tmtime.Now()}, false)

	// Retarget parts to the certificate/canonical header, as enterCommit does
	// when commit BlockID.PartSetHeader differs from the proposal.
	cs1.mtx.Lock()
	cs1.roundState.SetProposalBlock(nil)
	cs1.roundState.SetProposalBlockParts(types.NewPartSetFromHeader(canonicalParts.Header()))
	cs1.mtx.Unlock()

	for i := 0; i < int(canonicalParts.Total()); i++ {
		cs1.handleMsg(ctx, msgInfo{
			&BlockPartMessage{Height: height, Round: round, Part: canonicalParts.GetPart(i)},
			peerID,
			tmtime.Now(),
		}, false)
	}

	rs := cs1.GetRoundState()
	require.NotNil(t, rs.Proposal, "original proposal should remain")
	require.False(t, rs.Proposal.BlockID.PartSetHeader.Equals(canonicalParts.Header()))
	require.NotNil(t, rs.ProposalBlock, "canonical certificate parts must be accepted")
	require.True(t, rs.ProposalBlock.HashesTo(propBlock.Hash()))
	require.True(t, rs.ProposalBlockParts.HasHeader(canonicalParts.Header()))
}
