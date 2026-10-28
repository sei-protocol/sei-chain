package types

import (
	"encoding/hex"
	"slices"
	"testing"
	"time"

	"github.com/sei-protocol/sei-chain/sei-tendermint/internal/autobahn/pb"
	"github.com/sei-protocol/sei-chain/sei-tendermint/internal/protoutils"
	"github.com/sei-protocol/sei-chain/sei-tendermint/libs/utils"
	"github.com/sei-protocol/sei-chain/sei-tendermint/libs/utils/require"
)

func TestRoadBlockHeaderHashIndependentOfView(t *testing.T) {
	rng := utils.TestRng()
	committee, _ := GenCommittee(rng, 3)
	ep := GenEpochWithCommittee(rng, committee)
	proposal, blocks := ProposalAtBlocks(ep, View{Index: ep.RoadRange().First}, 2)
	wire := ProposalConv.Encode(proposal)
	wire.View.Number = utils.Alloc(uint64(proposal.View().Number.Next()))
	reproposal, err := ProposalConv.Decode(wire)
	require.NoError(t, err)
	require.Equal(t, proposal.Header().Hash(), reproposal.Header().Hash())
	require.NotEqual(t, NewHashed(proposal).Hash(), NewHashed(reproposal).Hash())

	key := GenSecretKey(rng)
	qc := NewCommitQC([]*Signed[*CommitVote]{Sign(key, NewCommitVote(proposal))})
	reproposalQC := NewCommitQC([]*Signed[*CommitVote]{Sign(key, NewCommitVote(reproposal))})
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
	require.NoError(t, RoadBlockHeaderConv.Test(header))
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

func TestProposalConvRequiresHeader(t *testing.T) {
	rng := utils.TestRng()
	wire := ProposalConv.Encode(GenProposal(rng))
	wire.Header = nil
	_, err := ProposalConv.Decode(wire)
	require.Error(t, err)
}

func TestRoadBlockHeaderConvRequiresTimestamp(t *testing.T) {
	_, err := RoadBlockHeaderConv.Decode(&pb.RoadBlockHeader{})
	require.Error(t, err)
}

func TestRoadBlockHeaderWireguardLaneRangeLimit(t *testing.T) {
	rng := utils.TestRng()
	laneRange := LaneRangeConv.Encode(GenLaneRange(rng))
	for _, count := range []int{MaxValidators, MaxValidators + 1} {
		ranges := make([]*pb.LaneRange, count)
		for i := range ranges {
			ranges[i] = laneRange
		}
		raw := protoutils.Marshal(&pb.Proposal{
			View:   ViewConv.Encode(GenView(rng)),
			Header: &pb.RoadBlockHeader{Timestamp: TimeConv.Encode(utils.GenTimestamp(rng)), LaneRanges: ranges},
		})
		_, err := protoutils.Unmarshal[*pb.Proposal](raw)
		if count == MaxValidators {
			require.NoError(t, err)
		} else {
			require.Error(t, err)
		}
	}
}
