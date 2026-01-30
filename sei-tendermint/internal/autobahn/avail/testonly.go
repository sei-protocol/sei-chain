package avail

import (
	"time"

	"github.com/tendermint/tendermint/internal/autobahn/types"
	"github.com/tendermint/tendermint/libs/utils"
)

func TestCommitQC(
	rng utils.Rng,
	committee *types.Committee,
	keys []types.SecretKey,
	prev utils.Option[*types.CommitQC],
	laneQCs map[types.LaneID]*types.LaneQC,
	appQC utils.Option[*types.AppQC],
) *types.CommitQC {
	fullProposal, err := types.NewProposal(
		types.TestSecretKey(types.GenNodeID(rng)),
		committee,
		types.ViewSpec{CommitQC: prev},
		time.Now(),
		laneQCs,
		appQC,
	)
	if err != nil {
		panic(err)
	}
	vote := types.NewCommitVote(fullProposal.Proposal().Msg())
	var votes []*types.Signed[*types.CommitVote]
	for _, k := range keys {
		votes = append(votes, types.Sign(k, vote))
	}
	return types.NewCommitQC(votes)
}
