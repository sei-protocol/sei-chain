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
}

type resourceMetrics struct {
	Receive *prometheus.Histogram
	Execute *prometheus.Histogram
}

type Metrics struct {
	Blocks resourceMetrics
	Txs    resourceMetrics
}

func Get() *Metrics {
	get := func(resource string) resourceMetrics {
		return resourceMetrics{
			Receive: Global.latencyAt(resource, "receive"),
			Execute: Global.latencyAt(resource, "execute"),
		}
	}
	return &Metrics{
		Blocks: get("blocks"),
		Txs:    get("txs"),
	}
}
