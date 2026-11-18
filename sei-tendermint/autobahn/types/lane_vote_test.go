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
	header := NewBlock(committee.Lanes().At(0), 0, BlockHeaderHash{}, GenPayload(rng)).Header()
	require.NoError(t, NewLaneVote(epoch.EpochIndex(), header).Verify(epoch))

	require.Error(t, NewLaneVote(epoch.EpochIndex()+1, header).Verify(epoch))
	badHeader := GenBlockHeader(rng)
	// Generated header is unrelated to committee, so it fails validation.
	require.Error(t, badHeader.Verify(committee))
	require.Error(t, NewLaneVote(epoch.EpochIndex(), badHeader).Verify(epoch))
}
