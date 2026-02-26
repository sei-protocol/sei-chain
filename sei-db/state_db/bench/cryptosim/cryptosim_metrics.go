package cryptosim

import (
	"context"
	"net/http"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promhttp"
)

// CryptosimMetrics holds Prometheus metrics for the cryptosim benchmark.
type CryptosimMetrics struct {
	reg                        *prometheus.Registry
	ctx                        context.Context
	blocksFinalizedTotal       prometheus.Counter
	transactionsProcessedTotal prometheus.Counter
	totalAccounts              prometheus.Gauge
	totalErc20Contracts        prometheus.Gauge
	dbCommitsTotal               prometheus.Counter
	mainThreadPhase              *PhaseTimer
	transactionPhaseTimerFactory *PhaseTimerFactory
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
	mainThreadPhase := NewPhaseTimer(reg, "main_thread")
	transactionPhaseTimerFactory := NewPhaseTimerFactory(reg, "transaction")

	reg.MustRegister(
		blocksFinalizedTotal,
		transactionsProcessedTotal,
		totalAccounts,
		totalErc20Contracts,
		dbCommitsTotal,
	)

	return &CryptosimMetrics{
		reg:                        reg,
		ctx:                        ctx,
		blocksFinalizedTotal:       blocksFinalizedTotal,
		transactionsProcessedTotal: transactionsProcessedTotal,
		totalAccounts:              totalAccounts,
		totalErc20Contracts:        totalErc20Contracts,
		dbCommitsTotal:               dbCommitsTotal,
		mainThreadPhase:              mainThreadPhase,
		transactionPhaseTimerFactory: transactionPhaseTimerFactory,
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
func (m *CryptosimMetrics) ReportBlockFinalized(transactionCount int64) {
	if m == nil {
		return
	}
	m.blocksFinalizedTotal.Inc()
	m.transactionsProcessedTotal.Add(float64(transactionCount))
}

// ReportDBCommit records that a database commit completed and the latency.
func (m *CryptosimMetrics) ReportDBCommit() {
	if m == nil {
		return
	}
	m.dbCommitsTotal.Inc()
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

// GetTransactionPhaseTimerInstance returns a new PhaseTimer from the transaction
// phase timer factory. Each call returns an independent timer; use one per
// transaction executor or thread.
func (m *CryptosimMetrics) GetTransactionPhaseTimerInstance() *PhaseTimer {
	if m == nil || m.transactionPhaseTimerFactory == nil {
		return nil
	}
	return m.transactionPhaseTimerFactory.Build()
}

// SetMainThreadPhase records a transition of the main thread to a new phase.
func (m *CryptosimMetrics) SetMainThreadPhase(phase string) {
	if m == nil {
		return
	}
	m.mainThreadPhase.SetPhase(phase)
}
