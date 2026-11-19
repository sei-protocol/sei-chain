package metrics

import (
	"time"

	"github.com/sei-protocol/sei-chain/sei-tendermint/libs/utils/prometheus"
)

const MetricsNamespace = "tendermint"
const MetricsSubsystem = "internal_autobahn_producer"

//go:generate go run github.com/sei-protocol/sei-chain/sei-tendermint/scripts/metricsgen -struct=metrics
type metrics struct {
	// Time an insert spent waiting for a CheckTx concurrency permit.
	checkTxWaitLatency prometheus.HistogramVec `metrics_buckets:"exp(0.00001, 2, 30)"`
	// Time an insert spent blocked waiting for mempool capacity.
	capacityWaitLatency prometheus.HistogramVec `metrics_buckets:"exp(0.0001, 2, 30)"`
	// Number of finished inserts by outcome.
	Inserts prometheus.CounterIntVec `metrics_labels:"result"`
}

// ObserveCheckTxWait records the time one insert waited for a CheckTx permit.
func ObserveCheckTxWait(d time.Duration) { Global.checkTxWaitLatencyAt().Observe(d.Seconds()) }

// ObserveCapacityWait records the time one insert waited for mempool capacity.
func ObserveCapacityWait(d time.Duration) { Global.capacityWaitLatencyAt().Observe(d.Seconds()) }
