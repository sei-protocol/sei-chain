package persist

import (
	"github.com/sei-protocol/sei-chain/sei-tendermint/libs/utils/prometheus"
)

const MetricsNamespace = "tendermint"
const MetricsSubsystem = "internal_autobahn_consensus_persist"

const (
	walBlocks    = "blocks"
	walCommitQCs = "commitqcs"

	stageAsked     = "asked"
	stagePersisted = "persisted"
)

//go:generate go run github.com/sei-protocol/sei-chain/sei-tendermint/scripts/metricsgen -struct=metrics
type metrics struct {
	// Records asked for append, or made durable.
	records prometheus.CounterIntVec `metrics_labels:"wal,stage"`
}

func addMetricsRecords(wal, stage string, n uint64) {
	if n == 0 {
		return
	}
	Global.recordsAt(wal, stage).Add(int64(n)) //nolint:gosec // a record count fits in int64
}
