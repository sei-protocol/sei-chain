package giga

import (
	"context"

	"github.com/sei-protocol/sei-chain/sei-tendermint/libs/utils/prometheus"
)

const MetricsNamespace = "tendermint"
const MetricsSubsystem = "p2p_giga"

const (
	resBlock        = "block"
	resFullCommitQC = "full_commit_qc"
	resAppQC        = "app_qc"
	resCommitQC     = "commit_qc"
	resLaneProposal = "lane_proposal"
)

const (
	votePrepare = "prepare"
	voteCommit  = "commit"
	voteTimeout = "timeout"
	voteApp     = "app"
	voteLane    = "lane"
	qcApp       = "app_qc"
)

//go:generate go run github.com/sei-protocol/sei-chain/sei-tendermint/scripts/metricsgen -struct=metrics
type metrics struct {
	// Client fetches by resource and reason, including ok.
	fetch prometheus.CounterIntVec `metrics_labels:"resource,reason"`
	// Server replies that did not return the requested object.
	serve prometheus.CounterIntVec `metrics_labels:"resource,reason"`
	// Votes/QCs sent on a giga stream, counted once per peer stream.
	votesSent prometheus.CounterIntVec `metrics_labels:"type"`
	// Votes/QCs decoded from a giga stream.
	votesReceived prometheus.CounterIntVec `metrics_labels:"type"`
}

func recordFetch(ctx context.Context, resource, reason string) {
	if ctx.Err() != nil {
		return
	}
	Global.fetchAt(resource, reason).Add(1)
}

func recordServe(resource, reason string) {
	Global.serveAt(resource, reason).Add(1)
}

func recordVoteSent(typ string) {
	Global.votesSentAt(typ).Add(1)
}

func recordVoteReceived(typ string) {
	Global.votesReceivedAt(typ).Add(1)
}
