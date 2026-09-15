package metrics

import (
	"time"

	"github.com/sei-protocol/sei-chain/sei-tendermint/libs/utils/prometheus"
)

const MetricsNamespace = "tendermint"
const MetricsSubsystem = "internal_autobahn_producer"

//go:generate go run github.com/sei-protocol/sei-chain/sei-tendermint/scripts/metricsgen -struct=metrics
type metrics struct {
	// Duration of the app CheckTx call made for each inserted tx.
	checkTxLatency prometheus.HistogramVec `metrics_buckets:"exp(0.00001, 2, 30)"`
	// Duration of the app nonce lookup made for a sender not yet tracked by the mempool.
	nonceLookupLatency prometheus.HistogramVec `metrics_buckets:"exp(0.00001, 2, 30)"`
	// Time an insert spent blocked waiting for mempool capacity.
	capacityWaitLatency prometheus.HistogramVec `metrics_buckets:"exp(0.0001, 2, 30)"`
	// Time an insert spent acquiring and holding the mempool lock, excluding time spent waiting for capacity.
	admitLatency prometheus.HistogramVec `metrics_buckets:"exp(0.000001, 2, 30)"`
	// Number of times a blocked insert woke up and found the mempool still full.
	capacityWaitWakeups prometheus.CounterIntVec
	// Number of inserts currently in the given phase.
	inFlight prometheus.GaugeIntVec `metrics_labels:"phase"`
	// Number of finished inserts by outcome.
	inserts prometheus.CounterIntVec `metrics_labels:"result"`
}

// Phase is an insert phase tracked by the in-flight gauge.
type Phase struct{ gauge *prometheus.GaugeInt }

var (
	PhaseCheckTx      = Phase{Global.inFlightAt("check_tx")}
	PhaseCapacityWait = Phase{Global.inFlightAt("capacity_wait")}
	PhaseAdmit        = Phase{Global.inFlightAt("admit")}
)

// Enter marks an insert entering the phase and returns the function marking it leaving.
func (p Phase) Enter() func() {
	p.gauge.Add(1)
	return func() { p.gauge.Add(-1) }
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

// ObserveCheckTx records the duration of one CheckTx call.
func ObserveCheckTx(d time.Duration) { Global.checkTxLatencyAt().Observe(d.Seconds()) }

// ObserveNonceLookup records the duration of one app nonce lookup.
func ObserveNonceLookup(d time.Duration) { Global.nonceLookupLatencyAt().Observe(d.Seconds()) }

// ObserveCapacityWait records the total time one insert waited for capacity and how many times it woke up.
func ObserveCapacityWait(d time.Duration, wakeups int64) {
	Global.capacityWaitLatencyAt().Observe(d.Seconds())
	Global.capacityWaitWakeupsAt().Add(wakeups)
}

// ObserveAdmit records the time one insert spent in the locked admission section.
func ObserveAdmit(d time.Duration) { Global.admitLatencyAt().Observe(d.Seconds()) }
