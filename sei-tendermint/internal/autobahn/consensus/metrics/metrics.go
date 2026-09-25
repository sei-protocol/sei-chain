package metrics

import (
	"github.com/sei-protocol/sei-chain/sei-tendermint/autobahn/types"
	"github.com/sei-protocol/sei-chain/sei-tendermint/libs/utils/prometheus"
)

const MetricsNamespace = "tendermint"
const MetricsSubsystem = "internal_autobahn_consensus"

// TimeoutPhase is this replica's progress in a view when it timeout-votes.
type TimeoutPhase struct{ label string }

var (
	// PhaseNoProposal is a timeout vote with no prepare vote in this view.
	PhaseNoProposal = TimeoutPhase{"no_proposal"}
	// PhaseNoPrepareQC is a timeout vote after a prepare vote but no this-view PrepareQC.
	PhaseNoPrepareQC = TimeoutPhase{"no_prepare_qc"}
	// PhaseNoCommit is a timeout vote after a this-view PrepareQC.
	PhaseNoCommit = TimeoutPhase{"no_commit"}
)

//go:generate go run github.com/sei-protocol/sei-chain/sei-tendermint/scripts/metricsgen -struct=metrics
type metrics struct {
	// TimeoutQCs formed locally or applied from the network, labeled by leader address.
	timeouts prometheus.CounterIntVec `metrics_labels:"leader"`
	// Timeout votes this replica cast, labeled by leader address and local phase.
	timeoutVotes prometheus.CounterIntVec `metrics_labels:"leader,phase"`
	// CommitQCs admitted by avail, labeled by the committed view's leader address.
	commits prometheus.CounterIntVec `metrics_labels:"leader"`
	// Votes the aggregator accepted (new for that replica, or a strictly later view).
	votesIngested prometheus.CounterIntVec `metrics_labels:"type"`
	// View number of the current consensus view.
	viewNumber prometheus.GaugeIntVec
}

var (
	VotePrepare = "prepare"
	VoteCommit  = "commit"
	VoteTimeout = "timeout"
)

func leaderLabel(k types.PublicKey) string {
	return k.ED25519().Address().String()
}

// SetView records the view number of the current consensus view.
func SetView(v types.View) {
	Global.viewNumberAt().Set(int64(v.Number)) // nolint: gosec
}

// ObserveTimeout records that the given leader's view timed out.
func ObserveTimeout(leader types.PublicKey) {
	Global.timeoutsAt(leaderLabel(leader)).Add(1)
}

// ObserveTimeoutVote records that this replica timeout-voted the given leader's view.
func ObserveTimeoutVote(leader types.PublicKey, phase TimeoutPhase) {
	Global.timeoutVotesAt(leaderLabel(leader), phase.label).Add(1)
}

// ObserveCommit records that a proposal from the given leader committed.
func ObserveCommit(leader types.PublicKey) {
	Global.commitsAt(leaderLabel(leader)).Add(1)
}

// ObserveVoteIngested records that the aggregator accepted one vote of the given type.
func ObserveVoteIngested(typ string) {
	Global.votesIngestedAt(typ).Add(1)
}
