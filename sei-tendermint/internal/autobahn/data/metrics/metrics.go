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

type stageMetrics[T any] struct {
	Receive T 
	Execute T 
	Certify T 
}

func newStageMetrics[T any](gen func(stage string) T) stageMetrics[T] {
	return stageMetrics[T]{
		Receive: gen("receive"),
		Execute: gen("execute"),
		Certify: gen("certify"),
	}
}

type Metrics struct {
	BlockHeight stageMetrics[*prometheus.GaugeInt]
	Blocks stageMetrics[*prometheus.Histogram]
	Txs stageMetrics[*prometheus.Histogram]
	GasUsed *prometheus.CounterInt
	// TxSize has no finite buckets; it exports count and sum only.
	TxSize *prometheus.Histogram
}

func Get() *Metrics {
	return &Metrics{
		BlockHeight: newStageMetrics(Global.blockHeightAt),
		Blocks: newStageMetrics(func(stage string) *prometheus.Histogram { return Global.latencyAt("blocks",stage) }),
		Txs: newStageMetrics(func(stage string) *prometheus.Histogram { return Global.latencyAt("txs",stage) }),
		GasUsed: Global.gasUsedAt(),
		TxSize:  Global.txSizeAt(),
	}
}
