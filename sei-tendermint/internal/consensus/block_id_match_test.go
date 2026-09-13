package consensus

import (
	"bytes"
	"io"
	"testing"
	"time"

	"github.com/gogo/protobuf/proto"

	tmconfig "github.com/sei-protocol/sei-chain/sei-tendermint/config"
	"github.com/sei-protocol/sei-chain/sei-tendermint/crypto"
	tmtime "github.com/sei-protocol/sei-chain/sei-tendermint/libs/time"
	"github.com/sei-protocol/sei-chain/sei-tendermint/libs/utils"
	"github.com/sei-protocol/sei-chain/sei-tendermint/libs/utils/require"
	tmproto "github.com/sei-protocol/sei-chain/sei-tendermint/proto/tendermint/types"
	"github.com/sei-protocol/sei-chain/sei-tendermint/types"
	"github.com/sei-protocol/sei-chain/sei-tendermint/version"
)

func testBlock(t *testing.T) *types.Block {
	t.Helper()
	return testBlockWith(t, nil, nil)
}

func testBlockWith(t *testing.T, txs types.Txs, lastCommit *types.Commit) *types.Block {
	t.Helper()
	valHash := crypto.CRandBytes(32)
	if lastCommit == nil {
		lastCommit = &types.Commit{}
	}
	block := &types.Block{
		Header: types.Header{
			Version:            version.Consensus{Block: version.BlockProtocol, App: 1},
			ChainID:            "test-chain",
			Height:             1,
			ValidatorsHash:     valHash,
			NextValidatorsHash: valHash,
			ConsensusHash:      crypto.CRandBytes(32),
			AppHash:            crypto.CRandBytes(32),
			LastResultsHash:    crypto.CRandBytes(32),
			ProposerAddress:    crypto.CRandBytes(crypto.AddressSize),
		},
		Data:       types.Data{Txs: txs},
		LastCommit: lastCommit,
	}
	block.LastCommitHash = block.LastCommit.Hash()
	block.DataHash = block.Data.Hash(false)
	block.EvidenceHash = block.Evidence.Hash()
	require.NotNil(t, block.Hash())
	return block
}

// nonCanonicalBlockBytes returns block's canonical protobuf encoding and that
// encoding with an unknown length-delimited protobuf field (999) appended.
func nonCanonicalBlockBytes(t *testing.T, block *types.Block) (canonical, nonCanonical []byte) {
	t.Helper()
	pbb, err := block.ToProto()
	require.NoError(t, err)
	canonical, err = proto.Marshal(pbb)
	require.NoError(t, err)
	nonCanonical = append(append([]byte{}, canonical...), 0xba, 0x3e, 0x04, 'j', 'u', 'n', 'k')
	return canonical, nonCanonical
}

func nonCanonicalPartSet(t *testing.T, block *types.Block) *types.PartSet {
	t.Helper()
	_, nonCanonical := nonCanonicalBlockBytes(t, block)
	parts := types.NewPartSetFromData(nonCanonical, types.BlockPartSizeBytes)
	canonical, err := block.MakePartSet(types.BlockPartSizeBytes)
	require.NoError(t, err)
	require.False(t, parts.Header().Equals(canonical.Header()))
	return parts
}

func TestBlockIDMatches(t *testing.T) {
	block := testBlock(t)
	hash := block.Hash()
	parts, err := block.MakePartSet(types.BlockPartSizeBytes)
	require.NoError(t, err)

	matching := types.BlockID{Hash: hash, PartSetHeader: parts.Header()}
	require.True(t, blockIDMatches(block, parts, matching))

	wrongParts := types.BlockID{
		Hash: hash,
		PartSetHeader: types.PartSetHeader{
			Total: parts.Total(),
			Hash:  crypto.CRandBytes(32),
		},
	}
	require.False(t, blockIDMatches(block, parts, wrongParts))

	wrongHash := types.BlockID{
		Hash:          crypto.CRandBytes(32),
		PartSetHeader: parts.Header(),
	}
	require.False(t, blockIDMatches(block, parts, wrongHash))
	require.False(t, blockIDMatches(nil, parts, matching))
	require.False(t, blockIDMatches(block, nil, matching))
	require.False(t, blockIDMatches(block, parts, types.BlockID{}))
	require.False(t, blockIDMatches(block, parts, types.BlockID{
		Hash: hash, // missing PartSetHeader → incomplete
	}))
}

func TestProposalMatchesLocked(t *testing.T) {
	block := testBlock(t)
	parts, err := block.MakePartSet(types.BlockPartSizeBytes)
	require.NoError(t, err)

	require.True(t, proposalMatchesLocked(block, block, parts, parts))

	otherPartsHeader := types.PartSetHeader{Total: 1, Hash: crypto.CRandBytes(32)}
	otherParts := types.NewPartSetFromHeader(otherPartsHeader)
	require.False(t, proposalMatchesLocked(block, block, parts, otherParts))
}

func TestNonCanonicalPartSetSameHeaderHash(t *testing.T) {
	block := testBlock(t)
	canonical, err := block.MakePartSet(types.BlockPartSizeBytes)
	require.NoError(t, err)

	_, nonCanonical := nonCanonicalBlockBytes(t, block)
	nonCanonicalParts := types.NewPartSetFromData(nonCanonical, types.BlockPartSizeBytes)
	require.False(t, nonCanonicalParts.Header().Equals(canonical.Header()))

	var pbb tmproto.Block
	require.NoError(t, proto.Unmarshal(nonCanonical, &pbb))
	decoded, err := types.BlockFromProto(&pbb)
	require.NoError(t, err)
	require.True(t, decoded.HashesTo(block.Hash()), "logical header hash unchanged")
	require.False(t, blockIDMatches(decoded, nonCanonicalParts, types.BlockID{
		Hash:          block.Hash(),
		PartSetHeader: canonical.Header(),
	}))
	require.True(t, blockIDMatches(decoded, nonCanonicalParts, types.BlockID{
		Hash:          block.Hash(),
		PartSetHeader: nonCanonicalParts.Header(),
	}))
}

func TestCanonicalPartBytesRoundTripShapes(t *testing.T) {
	ctx := t.Context()
	config := configSetup(t)
	cs, _ := makeState(ctx, t, makeStateArgs{config: config, validators: 2})

	valAddr := crypto.CRandBytes(crypto.AddressSize)
	sig := utils.OrPanic1(crypto.SigFromBytes(crypto.CRandBytes(64)))
	mixedCommit := &types.Commit{
		Height: 1,
		Round:  0,
		BlockID: types.BlockID{
			Hash: crypto.CRandBytes(32),
			PartSetHeader: types.PartSetHeader{
				Total: 1,
				Hash:  crypto.CRandBytes(32),
			},
		},
		Signatures: []types.CommitSig{
			{
				BlockIDFlag:      types.BlockIDFlagCommit,
				ValidatorAddress: valAddr,
				Timestamp:        cs.state.LastBlockTime,
				Signature:        utils.Some(sig),
			},
			types.NewCommitSigAbsent(),
		},
	}

	dve, err := types.NewMockDuplicateVoteEvidence(ctx, 1, time.Now(), "test-chain")
	require.NoError(t, err)
	withEvidence := testBlock(t)
	withEvidence.Evidence = types.EvidenceList{dve}
	withEvidence.EvidenceHash = withEvidence.Evidence.Hash()

	// BlockPartSizeBytes is 1MiB; one oversized tx forces Total() > 1.
	multiPart := testBlockWith(t, types.Txs{make([]byte, int(types.BlockPartSizeBytes)+1)}, nil)

	cases := []struct {
		name      string
		block     *types.Block
		multiPart bool
	}{
		{name: "empty", block: testBlock(t)},
		{name: "with_txs", block: testBlockWith(t, types.Txs{[]byte("tx-a"), []byte("tx-b")}, nil)},
		{name: "mixed_last_commit", block: testBlockWith(t, types.Txs{[]byte("tx")}, mixedCommit)},
		{name: "duplicate_vote_evidence", block: withEvidence},
		{name: "multi_part", block: multiPart, multiPart: true},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			parts, err := tc.block.MakePartSet(types.BlockPartSizeBytes)
			require.NoError(t, err)
			if tc.multiPart {
				require.Greater(t, parts.Total(), uint32(1))
			}

			// Production path: assemble part bytes → decode → remake PartSetHeader.
			// Comparing MakePartSet(tc.block) to itself would be a tautology; the
			// invariant is that MakePartSet(decoded) reproduces the proposer's header.
			bz, err := io.ReadAll(parts.GetReader())
			require.NoError(t, err)
			var pbb tmproto.Block
			require.NoError(t, proto.Unmarshal(bz, &pbb))
			decoded, err := types.BlockFromProto(&pbb)
			require.NoError(t, err)

			cs.roundState.SetProposal(nil)
			cs.roundState.SetProposalBlockParts(parts)
			require.NoError(t, cs.verifyCanonicalProposalParts(decoded))

			canonical, nonCanonical := nonCanonicalBlockBytes(t, tc.block)
			require.True(t, bytes.Equal(bz, canonical))
			cs.roundState.SetProposalBlockParts(types.NewPartSetFromData(nonCanonical, types.BlockPartSizeBytes))
			err = cs.verifyCanonicalProposalParts(decoded)
			require.ErrorIs(t, err, ErrNonCanonicalProposalParts)
		})
	}
}

// Same canonical bytes with non-default part size yield a different PartSetHeader
// and must be rejected (blocksync rebuilds with BlockPartSizeBytes).
func TestRejectNonDefaultPartChunking(t *testing.T) {
	ctx := t.Context()
	config := configSetup(t)
	cs, _ := makeState(ctx, t, makeStateArgs{config: config, validators: 2})

	// Large enough that a smaller part size splits into multiple parts.
	block := testBlockWith(t, types.Txs{make([]byte, 8*1024)}, nil)
	pbb, err := block.ToProto()
	require.NoError(t, err)
	bz, err := proto.Marshal(pbb)
	require.NoError(t, err)

	altPartSize := uint32(512)
	require.Greater(t, len(bz), int(altPartSize))
	altParts := types.NewPartSetFromData(bz, altPartSize)
	canonical, err := block.MakePartSet(types.BlockPartSizeBytes)
	require.NoError(t, err)
	require.False(t, altParts.Header().Equals(canonical.Header()))
	require.Greater(t, altParts.Total(), uint32(1))

	cs.roundState.SetProposal(nil)
	cs.roundState.SetProposalBlockParts(altParts)
	err = cs.verifyCanonicalProposalParts(block)
	require.ErrorIs(t, err, ErrNonCanonicalProposalParts)
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

	cs1.handleMsg(ctx, msgInfo{&ProposalMessage{proposal}, peerID, tmtime.Now()}, false)
	require.Greater(t, nonCanonicalParts.Total(), uint32(0))

	// Deliver all but the last part without completing.
	for i := 0; i < int(nonCanonicalParts.Total())-1; i++ {
		cs1.handleMsg(ctx, msgInfo{
			&BlockPartMessage{Height: height, Round: round, Part: nonCanonicalParts.GetPart(i)},
			peerID,
			tmtime.Now(),
		}, false)
	}

	cs1.mtx.Lock()
	added, err := cs1.addProposalBlockPart(&BlockPartMessage{
		Height: height,
		Round:  round,
		Part:   nonCanonicalParts.GetPart(int(nonCanonicalParts.Total()) - 1),
	}, peerID)
	cs1.mtx.Unlock()

	require.False(t, added)
	require.ErrorIs(t, err, ErrNonCanonicalProposalParts)
	require.Nil(t, cs1.GetRoundState().ProposalBlock, "proposal block must not be accepted from non-canonical parts")
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

func TestEnterPrecommitDoesNotRelockOnPartSetMismatch(t *testing.T) {
	config := configSetup(t)
	chainID := tmconfig.TestLoadGenesis(config).ChainID
	ctx := t.Context()

	cs1, vss := makeState(ctx, t, makeStateArgs{config: config, validators: 4})
	height, round := cs1.roundState.Height(), cs1.roundState.Round()
	voteCh := cs1.subscribeToVoterBuffered(ctx, t, cs1.address(ctx))

	propBlock, err := cs1.createProposalBlock(ctx)
	require.NoError(t, err)
	propBlockParts, err := propBlock.MakePartSet(types.BlockPartSizeBytes)
	require.NoError(t, err)
	blockID := types.BlockID{Hash: propBlock.Hash(), PartSetHeader: propBlockParts.Header()}

	pubKey, err := vss[0].PrivValidator.GetPubKey(ctx)
	require.NoError(t, err)
	proposal := types.NewProposal(
		height, round, -1, blockID, propBlock.Time,
		propBlock.GetTxHashes(), propBlock.Header, propBlock.LastCommit, propBlock.Evidence, pubKey.Address(),
	)
	p := proposal.ToProto()
	require.NoError(t, vss[0].SignProposal(ctx, chainID, p))
	proposal.Signature = utils.OrPanic1(crypto.SigFromBytes(p.Signature))

	cs1.startTestRound(ctx, height, round)
	peerID, err := types.NewNodeID("AAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA")
	require.NoError(t, err)

	cs1.handleMsg(ctx, msgInfo{&ProposalMessage{proposal}, peerID, tmtime.Now()}, false)
	for i := 0; i < int(propBlockParts.Total()); i++ {
		cs1.handleMsg(ctx, msgInfo{
			&BlockPartMessage{Height: height, Round: round, Part: propBlockParts.GetPart(i)},
			peerID,
			tmtime.Now(),
		}, false)
	}
	ensurePrevote(t, voteCh, height, round)

	cs1.mtx.Lock()
	cs1.roundState.SetLockedRound(round)
	cs1.roundState.SetLockedBlock(propBlock)
	cs1.roundState.SetLockedBlockParts(propBlockParts)

	mismatchedID := types.BlockID{
		Hash: propBlock.Hash(),
		PartSetHeader: types.PartSetHeader{
			Total: propBlockParts.Total(),
			Hash:  crypto.CRandBytes(32),
		},
	}
	require.False(t, blockIDMatches(propBlock, propBlockParts, mismatchedID))

	// Inject maj23 directly so tryAddVote does not clear ProposalBlock before
	// enterPrecommit; we want the lock/proposal BlockID match path.
	for _, vs := range vss[1:] {
		vote := signVote(ctx, t, vs, tmproto.PrevoteType, chainID, mismatchedID)
		added, err := cs1.roundState.Votes().AddVote(vote, peerID)
		require.NoError(t, err)
		require.True(t, added)
	}
	require.NotNil(t, cs1.roundState.ProposalBlock())
	cs1.enterPrecommit(ctx, height, round, "test-partset-mismatch")
	cs1.mtx.Unlock()

	// Hash matches lock but PartSetHeader does not → precommit nil, remain locked.
	ensurePrecommitMatch(t, voteCh, height, round, nil)
	cs1.validatePrecommit(ctx, t, round, round, vss[0], nil, propBlock.Hash())
}

// A proposal whose BlockID.Hash differs from the assembled block, but whose
// PartSetHeader matches the canonical part set, must still assemble.
func TestAssembleDespiteProposalHashMismatch(t *testing.T) {
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

	badHash := crypto.CRandBytes(32)
	require.False(t, propBlock.HashesTo(badHash))
	blockID := types.BlockID{Hash: badHash, PartSetHeader: canonicalParts.Header()}
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
	for i := 0; i < int(canonicalParts.Total()); i++ {
		cs1.handleMsg(ctx, msgInfo{
			&BlockPartMessage{Height: height, Round: round, Part: canonicalParts.GetPart(i)},
			peerID,
			tmtime.Now(),
		}, false)
	}

	rs := cs1.GetRoundState()
	require.NotNil(t, rs.Proposal)
	require.NotNil(t, rs.ProposalBlock, "proposal BlockID.Hash mismatch must not block assembly of canonical parts")
	require.True(t, rs.ProposalBlock.HashesTo(propBlock.Hash()))
	require.False(t, bytes.Equal(rs.Proposal.BlockID.Hash, propBlock.Hash()))
	require.True(t, rs.ProposalBlockParts.HasHeader(canonicalParts.Header()))
}
