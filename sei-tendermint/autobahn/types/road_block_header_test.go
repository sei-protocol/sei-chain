package types

import (
	"encoding/hex"
	"slices"
	"testing"
	"time"

	"github.com/sei-protocol/sei-chain/sei-tendermint/internal/autobahn/pb"
	"github.com/sei-protocol/sei-chain/sei-tendermint/libs/utils"
	"github.com/sei-protocol/sei-chain/sei-tendermint/libs/utils/require"
)

func TestRoadBlockHeaderHashIndependentOfView(t *testing.T) {
	rng := utils.TestRng()
	committee, _ := GenCommittee(rng, 3)
	ep := GenEpochWithCommittee(rng, committee)
	proposal, blocks := ProposalAtBlocks(ep, View{Index: ep.RoadRange().First}, 2)
	wire := ProposalConv.Encode(proposal)
	wire.ViewNumber = utils.Alloc(uint64(proposal.View().Number.Next()))
	reproposal, err := ProposalConv.Decode(wire)
	require.NoError(t, err)
	require.Equal(t, proposal.Header().Hash(), reproposal.Header().Hash())
	require.NotEqual(t, NewHashed(proposal).Hash(), NewHashed(reproposal).Hash())

	key := GenSecretKey(rng)
	qc := NewCommitQC([]*Signed[*CommitVote]{Sign(key, NewCommitVote(proposal.View().Number, proposal.Header()))})
	reproposalQC := NewCommitQC([]*Signed[*CommitVote]{Sign(key, NewCommitVote(reproposal.View().Number, reproposal.Header()))})
	require.Equal(t, proposal.Header().Hash(), qc.Header().Hash())
	require.Equal(t, qc.Header().Hash(), reproposalQC.Header().Hash())
	headers := make([]*BlockHeader, len(blocks))
	for i, block := range blocks {
		headers[i] = block.Header()
	}
	roadBlock := NewRoadBlock(NewFullCommitQC(qc, headers), blocks)
	require.Equal(t, proposal.Header(), roadBlock.Header())
	require.Equal(t, roadBlock.Header().Hash(), roadBlock.SuperBlock().Hash)
}

func TestRoadBlockHeaderHashCanonicalLaneRanges(t *testing.T) {
	rng := utils.TestRng()
	committee, keys := GenCommittee(rng, 3)
	ep := GenEpochWithCommittee(rng, committee)
	vs := ViewSpec{ConsensusSpec: ConsensusSpec{Epoch: ep}}
	proposal, err := NewProposal(leaderKey(committee, keys, vs.View()), vs, vs.NextTimestamp(), nil)
	require.NoError(t, err)
	header := proposal.Header()
	wire := RoadBlockHeaderConv.Encode(header)
	require.Len(t, wire.LaneRanges, 3)
	slices.Reverse(wire.LaneRanges)
	reordered, err := RoadBlockHeaderConv.Decode(wire)
	require.NoError(t, err)
	require.Equal(t, header.Hash(), reordered.Hash())

	wire.Timestamp = TimeConv.Encode(header.Timestamp().Add(time.Microsecond))
	changedTime, err := RoadBlockHeaderConv.Decode(wire)
	require.NoError(t, err)
	require.NotEqual(t, header.Hash(), changedTime.Hash())

	wire = RoadBlockHeaderConv.Encode(header)
	wire.LaneRanges[0].LastHash = slices.Clone(wire.LaneRanges[0].LastHash)
	wire.LaneRanges[0].LastHash[0] ^= 1
	changedRange, err := RoadBlockHeaderConv.Decode(wire)
	require.NoError(t, err)
	require.NotEqual(t, header.Hash(), changedRange.Hash())
}

func TestRoadBlockHeaderHashBytes(t *testing.T) {
	rng := utils.TestRng()
	hash := GenProposal(rng).Header().Hash()
	parsed, err := ParseRoadBlockHeaderHash(hash.Bytes())
	require.NoError(t, err)
	require.Equal(t, hash, parsed)
	require.Equal(t, hex.EncodeToString(hash.Bytes()), hash.String())
	for _, size := range []int{0, 31, 33} {
		_, err := ParseRoadBlockHeaderHash(make([]byte, size))
		require.Error(t, err)
	}
}

func TestRoadBlockHeaderIdentity(t *testing.T) {
	hashes := map[RoadBlockHeaderHash]struct{}{}
	for _, view := range []View{
		{},
		{EpochIndex: 7, Index: 29, Number: 13},
		{EpochIndex: 7, Index: 30, Number: 13},
		{EpochIndex: 8, Index: 29, Number: 13},
	} {
		proposal, err := ProposalConv.Decode(&pb.Proposal{
			ViewNumber: utils.Alloc(uint64(view.Number)),
			Header: &pb.RoadBlockHeader{
				EpochIndex: utils.Alloc(uint64(view.EpochIndex)),
				Index:      utils.Alloc(uint64(view.Index)),
				Timestamp:  TimeConv.Encode(time.Time{}),
			},
		})
		require.NoError(t, err)
		header := proposal.Header()
		require.Equal(t, view.EpochIndex, header.EpochIndex())
		require.Equal(t, view.Index, header.Index())
		require.Equal(t, view, proposal.View())
		require.Equal(t, view, NewPrepareVote(view.Number, header).View())
		require.Equal(t, view, NewCommitVote(view.Number, header).View())
		_, seen := hashes[header.Hash()]
		require.False(t, seen)
		hashes[header.Hash()] = struct{}{}
	}
}
