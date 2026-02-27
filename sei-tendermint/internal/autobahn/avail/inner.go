package avail

import (
	"fmt"

	"github.com/sei-protocol/sei-chain/sei-tendermint/internal/autobahn/types"
	"github.com/sei-protocol/sei-chain/sei-tendermint/libs/utils"
)

type inner struct {
	latestAppQC    utils.Option[*types.AppQC]
	latestCommitQC utils.AtomicSend[utils.Option[*types.CommitQC]]
	appVotes       *queue[types.GlobalBlockNumber, appVotes]
	commitQCs      *queue[types.RoadIndex, *types.CommitQC]
	blocks         map[types.LaneID]*queue[types.BlockNumber, *types.Signed[*types.LaneProposal]]
	votes          map[types.LaneID]*queue[types.BlockNumber, blockVotes]
}

func newInner(c *types.Committee) *inner {
	votes := map[types.LaneID]*queue[types.BlockNumber, blockVotes]{}
	blocks := map[types.LaneID]*queue[types.BlockNumber, *types.Signed[*types.LaneProposal]]{}
	for _, lane := range c.Lanes().All() {
		votes[lane] = newQueue[types.BlockNumber, blockVotes]()
		blocks[lane] = newQueue[types.BlockNumber, *types.Signed[*types.LaneProposal]]()
	}
	return &inner{
		latestAppQC:    utils.None[*types.AppQC](),
		latestCommitQC: utils.NewAtomicSend(utils.None[*types.CommitQC]()),
		appVotes:       newQueue[types.GlobalBlockNumber, appVotes](),
		commitQCs:      newQueue[types.RoadIndex, *types.CommitQC](),
		blocks:         blocks,
		votes:          votes,
	}
}

func (i *inner) laneQC(c *types.Committee, lane types.LaneID, n types.BlockNumber) (*types.LaneQC, bool) {
	for _, vs := range i.votes[lane].q[n].byHeader {
		if len(vs) >= c.LaneQuorum() {
			return types.NewLaneQC(vs[:c.LaneQuorum()]), true
		}
	}
	return nil, false
}

func (i *inner) prune(appQC *types.AppQC, commitQC *types.CommitQC) (bool, error) {
	idx := appQC.Proposal().RoadIndex()
	if idx != commitQC.Proposal().Index() {
		return false, fmt.Errorf("mismatched QCs: appQC index %v, commitQC index %v", idx, commitQC.Proposal().Index())
	}
	if idx < types.NextOpt(i.latestAppQC) {
		return false, nil
	}
	i.latestAppQC = utils.Some(appQC)
	i.commitQCs.prune(idx)
	if i.commitQCs.next == idx {
		i.commitQCs.pushBack(commitQC)
		i.latestCommitQC.Store(utils.Some(commitQC))
	}
	i.appVotes.prune(commitQC.GlobalRange().First)
	for lane := range i.votes {
		lr := commitQC.LaneRange(lane)
		i.votes[lr.Lane()].prune(lr.First())
		i.blocks[lr.Lane()].prune(lr.First())
	}
	return true, nil
}
