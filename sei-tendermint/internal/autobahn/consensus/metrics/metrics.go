package metrics

import (
	"github.com/sei-protocol/sei-chain/sei-tendermint/libs/utils/prometheus"
)

const MetricsNamespace = "tendermint"
const MetricsSubsystem = "internal_autobahn_consensus"

const (
	// PhaseNoProposal is a timeout vote with no prepare vote in this view.
	PhaseNoProposal = "no_proposal"
	// PhaseNoPrepareQC is a timeout vote after a prepare vote but no this-view PrepareQC.
	PhaseNoPrepareQC = "no_prepare_qc"
	// PhaseNoCommit is a timeout vote after a this-view PrepareQC.
	PhaseNoCommit = "no_commit"
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

const (
	VotePrepare = "prepare"
	VoteCommit  = "commit"
	VoteTimeout = "timeout"
)

// Metrics are the consensus instruments callers write directly.
type Metrics struct {
	ViewNumber    *prometheus.GaugeInt
	Timeouts      prometheus.CounterIntVec
	TimeoutVotes  prometheus.CounterIntVec
	Commits       prometheus.CounterIntVec
	VotesIngested prometheus.CounterIntVec
}

// Get returns the consensus instruments.
func Get() *Metrics {
	return &Metrics{
		ViewNumber:    Global.viewNumberAt(),
		Timeouts:      Global.timeouts,
		TimeoutVotes:  Global.timeoutVotes,
		Commits:       Global.commits,
		VotesIngested: Global.votesIngested,
	}
}
