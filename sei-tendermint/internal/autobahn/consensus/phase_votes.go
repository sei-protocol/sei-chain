package consensus

import (
	"github.com/sei-protocol/sei-chain/sei-tendermint/autobahn/types"
	"github.com/sei-protocol/sei-chain/sei-tendermint/libs/utils"
)

type spv = *types.Signed[*types.PrepareVote]
type scv = *types.Signed[*types.CommitVote]
type hpv = types.Hash[*types.PrepareVote]
type hcv = types.Hash[*types.CommitVote]

type prepareVotes = phaseVotes[spv, hpv, *types.PrepareQC]
type commitVotes = phaseVotes[scv, hcv, *types.CommitQC]
type timeoutVotes = phaseVotes[*types.FullTimeoutVote, types.View, *types.TimeoutQC]

// votePhase is the wiring of one consensus phase: how a vote of that phase yields the
// aggregator's inputs, and how a quorum of such votes becomes that phase's QC.
type votePhase[V any, B comparable, QC any] struct {
	key    func(V) types.PublicKey
	view   func(V) types.View
	bucket func(V) B
	quorum func(*types.Committee) uint64
	qcView func(QC) types.View
	newQC  func([]V) QC
}

// phaseVotes holds the votes of one consensus phase and publishes the QC they form.
type phaseVotes[V any, B comparable, QC any] struct {
	phase votePhase[V, B, QC]
	votes *voteAggregator[V, B]
	qc    utils.AtomicSend[utils.Option[QC]]
}

func newPhaseVotes[V any, B comparable, QC any](phase votePhase[V, B, QC]) *phaseVotes[V, B, QC] {
	return &phaseVotes[V, B, QC]{
		phase: phase,
		votes: newVoteAggregator[V, B](),
		qc:    utils.NewAtomicSend(utils.None[QC]()),
	}
}

// pushVerifiedVote inserts a vote the caller has already verified against c, publishing a QC
// once the vote completes a quorum at a view later than the last QC published.
func (p *phaseVotes[V, B, QC]) pushVerifiedVote(c *types.Committee, vote V) {
	ph := p.phase
	votes, ok := p.votes.pushVote(c, ph.key(vote), ph.view(vote), ph.bucket(vote), vote, ph.quorum(c)).Get()
	if !ok {
		return
	}
	// Construct a QC from the votes.
	if old, ok := p.qc.Load().Get(); ok && !ph.qcView(old).Less(ph.view(vote)) {
		return
	}
	p.qc.Store(utils.Some(ph.newQC(votes)))
}

// newPrepareVotes returns an empty prepare-phase vote aggregator.
func newPrepareVotes() *prepareVotes {
	return newPhaseVotes(votePhase[spv, hpv, *types.PrepareQC]{
		key:    func(v spv) types.PublicKey { return v.Key() },
		view:   func(v spv) types.View { return v.Msg().Proposal().View() },
		bucket: func(v spv) hpv { return v.Hash() },
		quorum: (*types.Committee).PrepareQuorum,
		qcView: func(qc *types.PrepareQC) types.View { return qc.Proposal().View() },
		newQC:  types.NewPrepareQC,
	})
}

// newCommitVotes returns an empty commit-phase vote aggregator.
func newCommitVotes() *commitVotes {
	return newPhaseVotes(votePhase[scv, hcv, *types.CommitQC]{
		key:    func(v scv) types.PublicKey { return v.Key() },
		view:   func(v scv) types.View { return v.Msg().Proposal().View() },
		bucket: func(v scv) hcv { return v.Hash() },
		quorum: (*types.Committee).CommitQuorum,
		qcView: func(qc *types.CommitQC) types.View { return qc.Proposal().View() },
		newQC:  types.NewCommitQC,
	})
}

// newTimeoutVotes returns an empty timeout-phase vote aggregator.
func newTimeoutVotes() *timeoutVotes {
	return newPhaseVotes(votePhase[*types.FullTimeoutVote, types.View, *types.TimeoutQC]{
		key:    func(v *types.FullTimeoutVote) types.PublicKey { return v.Vote().Key() },
		view:   (*types.FullTimeoutVote).View,
		bucket: (*types.FullTimeoutVote).View,
		quorum: (*types.Committee).TimeoutQuorum,
		qcView: (*types.TimeoutQC).View,
		newQC:  types.NewTimeoutQC,
	})
}
