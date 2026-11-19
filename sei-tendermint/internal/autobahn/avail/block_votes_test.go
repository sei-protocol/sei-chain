package avail

import (
	"testing"

	"github.com/sei-protocol/sei-chain/sei-tendermint/autobahn/types"
	"github.com/sei-protocol/sei-chain/sei-tendermint/libs/utils"
	"github.com/sei-protocol/sei-chain/sei-tendermint/libs/utils/require"
)

func TestBlockVotes_CountsLaneQCOncePerBlock(t *testing.T) {
	rng := utils.TestRng()
	keys := rng.GenSliceN(4, types.GenSecretKey)
	weights := make(map[types.PublicKey]uint64, len(keys))
	for _, k := range keys {
		weights[k.Public()] = 1
	}
	ep := types.NewEpoch(0, types.RoadRange{First: 0, Next: 10}, utils.OrPanic1(types.NewCommittee(weights)))
	lane := ep.Committee().Lane(keys[0].Public()).OrPanic("lane")
	h := types.NewLaneBlock(lane, 0, types.LaneBlockHeaderHash{}, &types.Payload{}).Header()

	bv := newBlockVotes()
	beforeQC := gathered(t, "tendermint_internal_autobahn_avail_lane_qcs", nil)
	beforeVotes := gathered(t, "tendermint_internal_autobahn_avail_lane_votes_ingested", nil)
	first := keys[0].Sign(types.NewLaneVote(ep.EpochIndex(), h))
	bv.pushVote(ep.Committee(), first)
	bv.pushVote(ep.Committee(), first)
	for _, k := range keys[1:] {
		bv.pushVote(ep.Committee(), k.Sign(types.NewLaneVote(ep.EpochIndex(), h)))
	}
	require.True(t, bv.qc.IsPresent())
	require.Equal(t, beforeQC+1, gathered(t, "tendermint_internal_autobahn_avail_lane_qcs", nil))
	require.Equal(t, beforeVotes+int64(len(keys)), gathered(t, "tendermint_internal_autobahn_avail_lane_votes_ingested", nil))

}
