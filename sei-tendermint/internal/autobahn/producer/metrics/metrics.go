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
	inserts prometheus.CounterIntVec `metrics_labels:"result"`
}

// Result is an insert outcome tracked by the inserts counter.
type Result struct{ counter *prometheus.CounterInt }

var (
	ResultOK           = Result{Global.insertsAt("ok")}
	ResultRejected     = Result{Global.insertsAt("rejected")}
	ResultTooLarge     = Result{Global.insertsAt("too_large")}
	ResultFull         = Result{Global.insertsAt("full")}
	ResultNotProducing = Result{Global.insertsAt("not_producing")}
	ResultBadNonce     = Result{Global.insertsAt("bad_nonce")}
	ResultError        = Result{Global.insertsAt("error")}
)

// Observe counts one insert finishing with this outcome.
func (r Result) Observe() { r.counter.Add(1) }

// ObserveCheckTxWait records the time one insert waited for a CheckTx permit.
func ObserveCheckTxWait(d time.Duration) { Global.checkTxWaitLatencyAt().Observe(d.Seconds()) }

// ObserveCapacityWait records the total time one insert waited for capacity.
func ObserveCapacityWait(d time.Duration, wakeups int64) {
	Global.capacityWaitLatencyAt().Observe(d.Seconds())
}
