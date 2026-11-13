package types

import (
	"testing"

	"github.com/sei-protocol/sei-chain/sei-tendermint/internal/autobahn/pb"
	"github.com/sei-protocol/sei-chain/sei-tendermint/libs/utils"
	"github.com/sei-protocol/sei-chain/sei-tendermint/libs/utils/require"
)

func TestLaneVoteAndQCVerifyEpoch(t *testing.T) {
	rng := utils.TestRng()
	committee, keys := GenCommittee(rng, 4)
	ep := GenEpochWithCommittee(rng, committee)
	next := NewEpoch(ep.EpochIndex()+1, ep.RoadRange(), committee)
	lane := committee.Lanes().At(0)
	header := NewBlock(lane, 0, GenBlockHeaderHash(rng), GenPayload(rng)).Header()
	vote := NewLaneVote(ep.EpochIndex(), header)
	require.Equal(t, ep.EpochIndex(), vote.EpochIndex())
	require.NoError(t, vote.Verify(ep))
	require.Error(t, vote.Verify(next))
	votes := make([]*Signed[*LaneVote], len(keys))
	for i, key := range keys {
		votes[i] = Sign(key, vote)
	}
	qc := NewLaneQC(votes)
	require.Equal(t, ep.EpochIndex(), qc.EpochIndex())
	require.NoError(t, qc.Verify(ep))
	require.Error(t, qc.Verify(next))

	otherCommittee := utils.OrPanic1(NewCommittee(map[PublicKey]uint64{GenPublicKey(rng): 1}))
	other := NewEpoch(ep.EpochIndex(), ep.RoadRange(), otherCommittee)
	require.Error(t, vote.Verify(other))
	require.Error(t, qc.Verify(other))
}

func TestLaneVoteSignatureBindsEpoch(t *testing.T) {
	rng := utils.TestRng()
	vote := GenLaneVote(rng)
	signed := Sign(GenSecretKey(rng), vote)
	require.NoError(t, signed.VerifySig())
	encoded := SignedLaneVoteConv.Encode(signed)
	encoded.Msg.EpochIndex = new(uint64(vote.EpochIndex() + 1))
	tampered, err := SignedLaneVoteConv.Decode(encoded)
	require.NoError(t, err)
	require.Error(t, tampered.VerifySig())
}

func TestLaneVoteConvRequiresEpochAndHeader(t *testing.T) {
	rng := utils.TestRng()
	header := GenBlockHeader(rng)
	_, err := LaneVoteConv.Decode(&pb.LaneVote{Header: BlockHeaderConv.Encode(header)})
	require.Error(t, err)
	_, err = LaneVoteConv.Decode(&pb.LaneVote{EpochIndex: new(uint64(0))})
	require.Error(t, err)
	vote, err := LaneVoteConv.Decode(&pb.LaneVote{EpochIndex: new(uint64(0)), Header: BlockHeaderConv.Encode(header)})
	require.NoError(t, err)
	require.Equal(t, EpochIndex(0), vote.EpochIndex())
}
