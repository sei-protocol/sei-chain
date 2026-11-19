package types

import (
	"testing"

	"github.com/sei-protocol/sei-chain/sei-tendermint/libs/utils"
	"github.com/sei-protocol/sei-chain/sei-tendermint/libs/utils/require"
)

func TestLaneVoteVerify(t *testing.T) {
	rng := utils.TestRng()
	committee, _ := GenCommittee(rng, 4)
	epoch := GenEpochWithCommittee(rng, committee)
	header := NewLaneBlock(committee.Lanes().At(0), 0, LaneBlockHeaderHash{}, GenPayload(rng)).Header()
	require.NoError(t, NewLaneVote(epoch.EpochIndex(), header).Verify(epoch))

	require.Error(t, NewLaneVote(epoch.EpochIndex()+1, header).Verify(epoch))
	badHeader := GenLaneBlockHeader(rng)
	// Generated header is unrelated to committee, so it fails validation.
	require.Error(t, badHeader.Verify(committee))
	require.Error(t, NewLaneVote(epoch.EpochIndex(), badHeader).Verify(epoch))
}
