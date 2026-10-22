package consensus

import (
	"context"
	"time"

	"github.com/sei-protocol/sei-chain/sei-tendermint/autobahn/types"
	"github.com/sei-protocol/sei-chain/sei-tendermint/internal/autobahn/avail"
	availmetrics "github.com/sei-protocol/sei-chain/sei-tendermint/internal/autobahn/avail/metrics"
	"github.com/sei-protocol/sei-chain/sei-tendermint/libs/utils"
)

// laneBlockStagesInterval is how often runLaneBlockStages samples the lane blocks in flight.
const laneBlockStagesInterval = 250 * time.Millisecond

// runLaneBlockStages publishes how many lane blocks this node holds at each consensus step.
func (s *State) runLaneBlockStages(ctx context.Context) error {
	for {
		availmetrics.ObserveLaneBlockStages(laneBlockStages(s.avail.LaneTips(), s.innerRecv.Load()))
		if err := utils.Sleep(ctx, laneBlockStagesInterval); err != nil {
			return err
		}
	}
}

// laneBlockStages counts the uncommitted lane blocks this node holds by the consensus step each is
// waiting on, so every held block lands in exactly one step. Blocks in the current view's proposal
// wait on its PrepareQC until this node holds one, then on the CommitQC. A block counts as
// committed once either avail's persisted CommitQC or consensus's latest CommitQC covers it.
func laneBlockStages(tips map[types.LaneID]avail.LaneTip, i inner) availmetrics.LaneBlockStages {
	proposal := utils.None[*types.Proposal]()
	prepared := false
	if qc, ok := i.PrepareQC.Get(); ok {
		proposal, prepared = utils.Some(qc.Proposal()), true
	} else if v, ok := i.PrepareVote.Get(); ok {
		proposal = utils.Some(v.Msg().Proposal())
	}
	var st availmetrics.LaneBlockStages
	for lane, t := range tips {
		committed := min(max(t.Committed, types.LaneRangeOpt(i.spec.CommitQC, lane).Next()), t.Held)
		proposed := committed
		if p, ok := proposal.Get(); ok {
			proposed = min(max(committed, p.LaneRange(lane).Next()), t.Held)
		}
		certified := max(t.Certified, proposed)
		inProposal := int64(proposed - committed) //nolint:gosec // G115: bounded by the lane window.
		if prepared {
			st.CommitQC += inProposal
		} else {
			st.PrepareQC += inProposal
		}
		st.Proposal += int64(certified - proposed) //nolint:gosec // G115: bounded by the lane window.
		st.LaneQC += int64(t.Held - certified)     //nolint:gosec // G115: bounded by the lane window.
	}
	return st
}
