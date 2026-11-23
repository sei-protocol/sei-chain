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
	// Latency from a proposal's construction to this replica casting its PrepareVote, and to it observing the PrepareQC and the CommitQC. It includes clock skew between the leader and this replica.
	proposalLatency prometheus.HistogramVec `metrics_labels:"stage" metrics_buckets:"exp(0.0005, 1.5, 30)"`
	// Lane block positions past the latest persisted CommitQC, by the consensus step they wait on. Positions in the current proposal count whether or not this replica holds those blocks.
	laneBlocksInFlight prometheus.GaugeIntVec `metrics_labels:"stage"`
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
	// Latency from a proposal's construction to this replica's PrepareVote, PrepareQC and CommitQC.
	PrepareVoteLatency *prometheus.Histogram
	PrepareQCLatency   *prometheus.Histogram
	CommitQCLatency    *prometheus.Histogram
	// Lane block positions waiting on a LaneQC, on a proposal, and in the current proposal on its
	// PrepareQC or CommitQC.
	LaneBlocksAwaitingLaneQC    *prometheus.GaugeInt
	LaneBlocksAwaitingProposal  *prometheus.GaugeInt
	LaneBlocksAwaitingPrepareQC *prometheus.GaugeInt
	LaneBlocksAwaitingCommitQC  *prometheus.GaugeInt
}

// Get returns the consensus instruments.
func Get() *Metrics {
	return &Metrics{
		ViewNumber:    Global.viewNumberAt(),
		Timeouts:      Global.timeouts,
		TimeoutVotes:  Global.timeoutVotes,
		Commits:       Global.commits,
		VotesIngested: Global.votesIngested,

		PrepareVoteLatency: Global.proposalLatencyAt("prepare_vote"),
		PrepareQCLatency:   Global.proposalLatencyAt("prepare_qc"),
		CommitQCLatency:    Global.proposalLatencyAt("commit_qc"),

		LaneBlocksAwaitingLaneQC:    Global.laneBlocksInFlightAt("lane_qc"),
		LaneBlocksAwaitingProposal:  Global.laneBlocksInFlightAt("proposal"),
		LaneBlocksAwaitingPrepareQC: Global.laneBlocksInFlightAt("prepare_qc"),
		LaneBlocksAwaitingCommitQC:  Global.laneBlocksInFlightAt("commit_qc"),
	}
}
