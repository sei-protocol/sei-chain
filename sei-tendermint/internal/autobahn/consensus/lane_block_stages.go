package consensus

import (
	"context"
	"time"

	"github.com/sei-protocol/sei-chain/sei-tendermint/autobahn/types"
	"github.com/sei-protocol/sei-chain/sei-tendermint/internal/autobahn/avail"
	"github.com/sei-protocol/sei-chain/sei-tendermint/libs/utils"
)

// laneBlockStagesInterval is how often runLaneBlockStages samples the lane blocks in flight.
const laneBlockStagesInterval = 250 * time.Millisecond

// runLaneBlockStages publishes how many lane block positions wait at each consensus step.
func (s *State) runLaneBlockStages(ctx context.Context) error {
	for {
		s.innerRecv.Load().laneBlockStages(s.avail.LaneTips()).publish()
		if err := utils.Sleep(ctx, laneBlockStagesInterval); err != nil {
			return err
		}
	}
}

// laneBlockStages counts lane block positions past the latest persisted CommitQC by the consensus step each waits on.
type laneBlockStages struct {
	laneQC    types.BlockNumber
	proposal  types.BlockNumber
	prepareQC types.BlockNumber
	commitQC  types.BlockNumber
}

// publish sets the lane_blocks_in_flight gauges to s.
func (s laneBlockStages) publish() {
	meters.LaneBlocksAwaitingLaneQC.Set(utils.Clamp[int64](s.laneQC))
	meters.LaneBlocksAwaitingProposal.Set(utils.Clamp[int64](s.proposal))
	meters.LaneBlocksAwaitingPrepareQC.Set(utils.Clamp[int64](s.prepareQC))
	meters.LaneBlocksAwaitingCommitQC.Set(utils.Clamp[int64](s.commitQC))
}

// laneBlockStages splits each lane's positions past its committed tip into consecutive ranges: the
// current view's proposal, then blocks with a LaneQC, then blocks this replica holds without one.
// Positions in the proposal wait on its PrepareQC until this replica holds one, then on the CommitQC.
func (i inner) laneBlockStages(tips map[types.LaneID]avail.LaneTip) laneBlockStages {
	proposal := utils.None[*types.Proposal]()
	prepared := false
	if qc, ok := i.PrepareQC.Get(); ok {
		proposal, prepared = utils.Some(qc.Proposal()), true
	} else if v, ok := i.PrepareVote.Get(); ok {
		proposal = utils.Some(v.Msg().Proposal())
	}
	var st laneBlockStages
	for lane, t := range tips {
		proposed := t.WithCommitQC
		if p, ok := proposal.Get(); ok {
			proposed = max(proposed, p.LaneRange(lane).Next())
		}
		withLaneQC := max(t.WithLaneQC, proposed)
		held := max(t.Held, withLaneQC)
		if prepared {
			st.commitQC += proposed - t.WithCommitQC
		} else {
			st.prepareQC += proposed - t.WithCommitQC
		}
		st.proposal += withLaneQC - proposed
		st.laneQC += held - withLaneQC
	}
	return st
}
