package shadowreplay

import (
	"fmt"
	"net/http"
	"os"
	"sync"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promhttp"
)

// Metrics exposes Prometheus counters and gauges for shadow replay progress
// and divergence tracking.
type Metrics struct {
	Height          prometheus.Gauge
	ChainTip        prometheus.Gauge
	BlocksBehind    prometheus.Gauge
	BlocksReplayed  prometheus.Counter
	BlocksPerSecond prometheus.Gauge

	DivergencesTotal *prometheus.CounterVec
	AppHashMismatch  prometheus.Counter
	TxResultMismatch prometheus.Counter
	ModuleDivergence *prometheus.CounterVec

	BlockExecDuration prometheus.Summary
	GasUsedTotal      prometheus.Counter

	registry *prometheus.Registry
	server   *http.Server
	stopOnce sync.Once
}

// NewMetrics registers Prometheus metrics using the given chain label.
func NewMetrics(chain string) *Metrics {
	reg := prometheus.NewRegistry()
	labels := prometheus.Labels{"chain": chain}

	m := &Metrics{registry: reg}

	m.Height = prometheus.NewGauge(prometheus.GaugeOpts{
		Name:        "shadow_replay_height",
		Help:        "Current block height of shadow replay",
		ConstLabels: labels,
	})
	m.ChainTip = prometheus.NewGauge(prometheus.GaugeOpts{
		Name:        "shadow_replay_chain_tip",
		Help:        "Latest known chain tip height",
		ConstLabels: labels,
	})
	m.BlocksBehind = prometheus.NewGauge(prometheus.GaugeOpts{
		Name:        "shadow_replay_blocks_behind",
		Help:        "Number of blocks behind the chain tip",
		ConstLabels: labels,
	})
	m.BlocksReplayed = prometheus.NewCounter(prometheus.CounterOpts{
		Name:        "shadow_replay_blocks_replayed_total",
		Help:        "Total blocks replayed",
		ConstLabels: labels,
	})
	m.BlocksPerSecond = prometheus.NewGauge(prometheus.GaugeOpts{
		Name:        "shadow_replay_blocks_per_second",
		Help:        "Current replay throughput",
		ConstLabels: labels,
	})
	m.DivergencesTotal = prometheus.NewCounterVec(prometheus.CounterOpts{
		Name:        "shadow_replay_divergences_total",
		Help:        "Total divergences detected by severity",
		ConstLabels: labels,
	}, []string{"severity"})
	m.AppHashMismatch = prometheus.NewCounter(prometheus.CounterOpts{
		Name:        "shadow_replay_app_hash_mismatches_total",
		Help:        "Total app hash mismatches",
		ConstLabels: labels,
	})
	m.TxResultMismatch = prometheus.NewCounter(prometheus.CounterOpts{
		Name:        "shadow_replay_tx_result_mismatches_total",
		Help:        "Total tx-level result mismatches",
		ConstLabels: labels,
	})
	m.ModuleDivergence = prometheus.NewCounterVec(prometheus.CounterOpts{
		Name:        "shadow_replay_module_divergences_total",
		Help:        "Total divergences by module",
		ConstLabels: labels,
	}, []string{"module"})
	m.BlockExecDuration = prometheus.NewSummary(prometheus.SummaryOpts{
		Name:        "shadow_replay_block_execution_seconds",
		Help:        "Block execution duration",
		ConstLabels: labels,
		Objectives:  map[float64]float64{0.5: 0.05, 0.9: 0.01, 0.99: 0.001},
	})
	m.GasUsedTotal = prometheus.NewCounter(prometheus.CounterOpts{
		Name:        "shadow_replay_gas_used_total",
		Help:        "Total gas used across all replayed blocks",
		ConstLabels: labels,
	})

	reg.MustRegister(
		m.Height, m.ChainTip, m.BlocksBehind,
		m.BlocksReplayed, m.BlocksPerSecond,
		m.DivergencesTotal, m.AppHashMismatch, m.TxResultMismatch,
		m.ModuleDivergence, m.BlockExecDuration, m.GasUsedTotal,
	)

	return m
}

// RecordBlock updates metrics after a block has been compared.
func (m *Metrics) RecordBlock(comp *BlockComparison) {
	m.Height.Set(float64(comp.Height))
	m.BlocksReplayed.Inc()
	m.BlockExecDuration.Observe(float64(comp.ElapsedMs) / 1000.0)
	m.GasUsedTotal.Add(float64(comp.GasUsedTotal))

	if !comp.AppHashMatch {
		m.AppHashMismatch.Inc()
	}

	for _, d := range comp.Divergences {
		m.DivergencesTotal.WithLabelValues(d.Severity).Inc()
		if d.Scope == ScopeTx {
			m.TxResultMismatch.Inc()
		}
		if d.Module != "" {
			m.ModuleDivergence.WithLabelValues(d.Module).Inc()
		}
	}
}

// Serve starts the Prometheus metrics HTTP server at the given address.
// Pass an empty string to disable. This method is non-blocking.
func (m *Metrics) Serve(addr string) error {
	if addr == "" {
		return nil
	}

	mux := http.NewServeMux()
	mux.Handle("/metrics", promhttp.HandlerFor(m.registry, promhttp.HandlerOpts{}))

	m.server = &http.Server{Addr: addr, Handler: mux}

	go func() {
		if err := m.server.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			fmt.Fprintf(os.Stderr, "shadow-replay metrics server error: %v\n", err)
		}
	}()

	return nil
}

// Stop gracefully shuts down the metrics HTTP server.
func (m *Metrics) Stop() {
	m.stopOnce.Do(func() {
		if m.server != nil {
			_ = m.server.Close()
		}
	})
}

// NoopMetrics returns a Metrics instance with isolated collectors not
// registered with any shared registry. Safe to create multiple times.
func NoopMetrics() *Metrics {
	return NewMetrics("noop")
}
