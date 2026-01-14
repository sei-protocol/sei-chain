package avail

import (
	"github.com/prometheus/client_golang/prometheus"

	"github.com/sei-protocol/sei-stream/pkg/metrics"
	"github.com/tendermint/tendermint/internal/autobahn/types"
)

// BlockStage is stage of block processing.
type BlockStage string

const (
	// BlockStageReceived .
	BlockStageReceived BlockStage = "received"
	// BlockStageCommitted .
	BlockStageCommitted BlockStage = "committed"
	// BlockStageExecuted .
	BlockStageExecuted BlockStage = "executed"
)

// KV implements metrics.Labels.
func (s BlockStage) KV() (string, string) {
	return "stage", string(s)
}

func blocksMetric(stage BlockStage, total uint64) prometheus.Metric {
	return metrics.Gauge(
		"sei_consensus_avail__blocks",
		"total number of blocks at each stage of processing",
		float64(total),
		stage,
	)
}

func commitQCsMetric(total uint64) prometheus.Metric {
	return metrics.Gauge(
		"sei_consensus_avail__commit_qcs",
		"total number of commit QCs",
		float64(total),
	)
}

var _ prometheus.Collector = (*State)(nil)

// Describe from prometheus.Collector.
func (s *State) Describe(chan<- *prometheus.Desc) {}

// Collect from prometheus.Collector.
func (s *State) Collect(m chan<- prometheus.Metric) {
	qc := s.LastCommitQC().Load()
	blocksReceived := uint64(0)
	blocksExecuted := uint64(0)
	for inner := range s.inner.Lock() {
		if appQC, ok := inner.latestAppQC.Get(); ok {
			blocksExecuted = uint64(appQC.Proposal().GlobalNumber()) + 1
		}
		for _, q := range inner.blocks {
			blocksReceived += uint64(q.next)
		}
	}
	m <- blocksMetric(BlockStageReceived, blocksReceived)
	m <- blocksMetric(BlockStageCommitted, uint64(types.GlobalRangeOpt(qc).Next))
	m <- blocksMetric(BlockStageExecuted, blocksExecuted)
	m <- commitQCsMetric(uint64(types.NextIndexOpt(qc)))
}
