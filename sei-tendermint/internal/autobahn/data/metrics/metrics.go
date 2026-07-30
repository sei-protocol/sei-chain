package metrics

import (
	"github.com/sei-protocol/sei-chain/sei-tendermint/libs/utils/prometheus"
)

const MetricsNamespace = "tendermint"
const MetricsSubsystem = "internal_autobahn_data"

//go:generate go run github.com/sei-protocol/sei-chain/sei-tendermint/scripts/metricsgen -struct=metrics
type metrics struct {
	// latency of resource processing up from production to the given stage
	latency prometheus.HistogramVec `metrics_labels:"resource,stage" metrics_buckets:"exp(0.001, 1.5, 30)"`
	// Latest height of blocks processed up to the given stage.
	blockHeight prometheus.GaugeIntVec `metrics_labels:"stage"`
	// gas used by executed blocks
	gasUsed prometheus.CounterIntVec
	// size of executed transactions in bytes
	txSize prometheus.HistogramVec `metrics_buckets:"none"`
}

type stageMetrics struct {
	TxsLatency    *prometheus.Histogram
	BlocksLatency *prometheus.Histogram
	BlockHeight   *prometheus.GaugeInt
}

type Metrics struct {
	Receive stageMetrics
	Persist stageMetrics
	Execute stageMetrics
	Evict   stageMetrics
	GasUsed *prometheus.CounterInt
	// TxSize has no finite buckets; it exports count and sum only.
	TxSize *prometheus.Histogram
}

func Get() *Metrics {
	get := func(stage string) stageMetrics {
		return stageMetrics{
			TxsLatency:    Global.latencyAt("txs", stage),
			BlocksLatency: Global.latencyAt("blocks", stage),
			BlockHeight:   Global.blockHeightAt(stage),
		}
	}
	return &Metrics{
		Receive: get("receive"),
		Persist: get("persist"),
		Execute: get("execute"),
		Evict:   get("evict"),
		GasUsed: Global.gasUsedAt(),
		TxSize:  Global.txSizeAt(),
	}
}
