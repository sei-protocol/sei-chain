package cryptosim

import (
	"context"
	"net/http"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promhttp"
)

// Phase identifies where the main thread is spending its time.
type Phase string

const (
	PhaseExecuting  Phase = "executing"
	PhaseFinalizing Phase = "finalizing"
	PhaseCommitting Phase = "committing"
)

// CryptosimMetrics holds Prometheus metrics for the cryptosim benchmark.
type CryptosimMetrics struct {
	reg                        *prometheus.Registry
	ctx                        context.Context
	blocksFinalizedTotal       prometheus.Counter
	blockFinalizationLatency   prometheus.Observer
	transactionsProcessedTotal prometheus.Counter
	totalAccounts              prometheus.Gauge
	totalErc20Contracts        prometheus.Gauge
	dbCommitsTotal             prometheus.Counter
	dbCommitLatency            prometheus.Observer
	phaseDurationTotal         *prometheus.CounterVec
	phaseDurationCount         *prometheus.CounterVec
	lastPhase                  Phase
	lastPhaseChangeTime        time.Time
}

// NewCryptosimMetrics creates metrics for the cryptosim benchmark. A dedicated
// registry is created internally. When ctx is cancelled, the metrics HTTP server
// (if started) is shut down gracefully.
func NewCryptosimMetrics(
	ctx context.Context,
	metricsAddr string,
) *CryptosimMetrics {
	reg := prometheus.NewRegistry()

	blocksFinalizedTotal := prometheus.NewCounter(prometheus.CounterOpts{
		Name: "cryptosim_blocks_finalized_total",
		Help: "Total number of blocks finalized",
	})
	blockFinalizationLatency := prometheus.NewHistogram(prometheus.HistogramOpts{
		Name:    "cryptosim_block_finalization_latency_seconds",
		Help:    "Time to finalize a block in seconds",
		Buckets: prometheus.ExponentialBucketsRange(0.001, 10, 12),
	})
	transactionsProcessedTotal := prometheus.NewCounter(prometheus.CounterOpts{
		Name: "cryptosim_transactions_processed_total",
		Help: "Total number of transactions processed",
	})
	totalAccounts := prometheus.NewGauge(prometheus.GaugeOpts{
		Name: "cryptosim_accounts_total",
		Help: "Total number of accounts",
	})
	totalErc20Contracts := prometheus.NewGauge(prometheus.GaugeOpts{
		Name: "cryptosim_erc20_contracts_total",
		Help: "Total number of ERC20 contracts",
	})
	dbCommitsTotal := prometheus.NewCounter(prometheus.CounterOpts{
		Name: "cryptosim_db_commits_total",
		Help: "Total number of database commits",
	})
	dbCommitLatency := prometheus.NewHistogram(prometheus.HistogramOpts{
		Name:    "cryptosim_db_commit_latency_seconds",
		Help:    "Time to commit to the database in seconds",
		Buckets: prometheus.ExponentialBucketsRange(0.001, 10, 12),
	})
	phaseDurationTotal := prometheus.NewCounterVec(prometheus.CounterOpts{
		Name: "cryptosim_phase_duration_seconds_total",
		Help: "Total seconds spent in each phase (executing, finalizing, committing, etc.)",
	}, []string{"phase"})
	phaseDurationCount := prometheus.NewCounterVec(prometheus.CounterOpts{
		Name: "cryptosim_phase_duration_count_total",
		Help: "Total number of times each phase was reported",
	}, []string{"phase"})

	reg.MustRegister(
		blocksFinalizedTotal,
		blockFinalizationLatency,
		transactionsProcessedTotal,
		totalAccounts,
		totalErc20Contracts,
		dbCommitsTotal,
		dbCommitLatency,
		phaseDurationTotal,
		phaseDurationCount,
	)

	return &CryptosimMetrics{
		reg:                        reg,
		ctx:                        ctx,
		blocksFinalizedTotal:       blocksFinalizedTotal,
		blockFinalizationLatency:   blockFinalizationLatency,
		transactionsProcessedTotal: transactionsProcessedTotal,
		totalAccounts:              totalAccounts,
		totalErc20Contracts:        totalErc20Contracts,
		dbCommitsTotal:             dbCommitsTotal,
		dbCommitLatency:            dbCommitLatency,
		phaseDurationTotal:         phaseDurationTotal,
		phaseDurationCount:         phaseDurationCount,
	}
}

// StartServer starts the metrics HTTP server. Call this after loading initial
// state and setting gauges (e.g., SetTotalNumberOfAccounts) to avoid spurious
// rate spikes on restart. If addr is empty, no server is started.
func (m *CryptosimMetrics) StartServer(addr string) {
	if m == nil || addr == "" {
		return
	}
	startMetricsServer(m.ctx, m.reg, addr)
}

// startMetricsServer starts an HTTP server serving /metrics from reg. When ctx is
// cancelled, the server is shut down gracefully.
func startMetricsServer(ctx context.Context, reg *prometheus.Registry, addr string) {
	mux := http.NewServeMux()
	mux.Handle("/metrics", promhttp.HandlerFor(reg, promhttp.HandlerOpts{}))
	srv := &http.Server{Addr: addr, Handler: mux}
	go func() {
		_ = srv.ListenAndServe()
	}()
	go func() {
		<-ctx.Done()
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		_ = srv.Shutdown(shutdownCtx)
	}()
}

// ReportBlockFinalized records that a block was finalized, the number of
// transactions in that block, and the finalization latency.
func (m *CryptosimMetrics) ReportBlockFinalized(latency time.Duration, transactionCount int64) {
	if m == nil {
		return
	}
	m.blocksFinalizedTotal.Inc()
	m.transactionsProcessedTotal.Add(float64(transactionCount))
	m.blockFinalizationLatency.Observe(latency.Seconds())
}

// ReportDBCommit records that a database commit completed and the latency.
func (m *CryptosimMetrics) ReportDBCommit(latency time.Duration) {
	if m == nil {
		return
	}
	m.dbCommitsTotal.Inc()
	m.dbCommitLatency.Observe(latency.Seconds())
}

// SetTotalNumberOfAccounts sets the total number of accounts (e.g., when loading
// from existing data).
func (m *CryptosimMetrics) SetTotalNumberOfAccounts(total int64) {
	if m == nil {
		return
	}
	m.totalAccounts.Set(float64(total))
}

// IncrementTotalNumberOfAccounts records that a new account was created.
func (m *CryptosimMetrics) IncrementTotalNumberOfAccounts() {
	if m == nil {
		return
	}
	m.totalAccounts.Inc()
}

// SetTotalNumberOfERC20Contracts sets the total number of ERC20 contracts (e.g.,
// when loading from existing data).
func (m *CryptosimMetrics) SetTotalNumberOfERC20Contracts(total int64) {
	if m == nil {
		return
	}
	m.totalErc20Contracts.Set(float64(total))
}

// SetPhase records a transition of the main thread to a new phase.
//
// SetPhase is not safe for concurrent use.
func (m *CryptosimMetrics) SetPhase(phase Phase) {
	if m == nil || phase == "" {
		return
	}
	now := time.Now()
	if m.lastPhase != "" {
		latency := now.Sub(m.lastPhaseChangeTime)
		m.phaseDurationTotal.WithLabelValues(string(m.lastPhase)).Add(latency.Seconds())
		m.phaseDurationCount.WithLabelValues(string(m.lastPhase)).Inc()
	}
	m.lastPhase = phase
	m.lastPhaseChangeTime = now
}
