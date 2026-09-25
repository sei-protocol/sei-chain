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

//go:generate go run github.com/sei-protocol/sei-chain/sei-tendermint/scripts/metricsgen -struct=metrics
type metrics struct {
	// Client fetches by resource and reason, including ok.
	fetch prometheus.CounterIntVec `metrics_labels:"resource,reason"`
	// Server replies that did not return the requested object.
	serve prometheus.CounterIntVec `metrics_labels:"resource,reason"`
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
