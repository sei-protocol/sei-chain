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
	blocksFinalizedTotal     prometheus.Counter
	blockFinalizationLatency prometheus.Observer
	dbCommitsTotal           prometheus.Counter
	dbCommitLatency          prometheus.Observer
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
	dbCommitsTotal := prometheus.NewCounter(prometheus.CounterOpts{
		Name: "cryptosim_db_commits_total",
		Help: "Total number of database commits",
	})
	dbCommitLatency := prometheus.NewHistogram(prometheus.HistogramOpts{
		Name:    "cryptosim_db_commit_latency_seconds",
		Help:    "Time to commit to the database in seconds",
		Buckets: prometheus.ExponentialBucketsRange(0.001, 10, 12),
	})

	reg.MustRegister(blocksFinalizedTotal, blockFinalizationLatency, dbCommitsTotal, dbCommitLatency)

	if metricsAddr != "" {
		startMetricsServer(ctx, reg, metricsAddr)
	}

	return &CryptosimMetrics{
		blocksFinalizedTotal:     blocksFinalizedTotal,
		blockFinalizationLatency: blockFinalizationLatency,
		dbCommitsTotal:           dbCommitsTotal,
		dbCommitLatency:          dbCommitLatency,
	}
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

// ReportBlockFinalized records that a block was finalized and the latency.
func (m *CryptosimMetrics) ReportBlockFinalized(latency time.Duration) {
	if m == nil {
		return
	}
	m.blocksFinalizedTotal.Inc()
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
