package cryptosim

import (
	"context"
	"time"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/metric"

	"github.com/sei-protocol/sei-chain/sei-db/common/metrics"
)

const cryptosimMeterName = "cryptosim"

var receiptWriteLatencyBuckets = []float64{
	0.001, 0.0025, 0.005, 0.0075, 0.01,
	0.015, 0.02, 0.03, 0.05, 0.075,
	0.1, 0.25, 0.5, 0.75, 1, 2.5, 5,
}

// CryptosimMetrics holds OpenTelemetry metrics for the cryptosim benchmark.
// Metrics are exported via whatever exporter is configured on the global OTel
// MeterProvider (e.g., Prometheus, OTLP). This package does not import Prometheus.
type CryptosimMetrics struct {
	ctx context.Context

	blocksFinalizedTotal       metric.Int64Counter
	transactionsProcessedTotal metric.Int64Counter
	totalAccounts              metric.Int64Gauge
	hotAccounts                metric.Int64Gauge
	coldAccounts               metric.Int64Gauge
	dormantAccounts            metric.Int64Gauge
	totalErc20Contracts        metric.Int64Gauge
	dbCommitsTotal             metric.Int64Counter

	// Receipt metrics
	receiptBlockWriteDuration metric.Float64Histogram
	receiptChannelDepth       metric.Int64Gauge
	receiptsWrittenTotal      metric.Int64Counter
	receiptErrorsTotal        metric.Int64Counter

	mainThreadPhase              *metrics.PhaseTimer
	transactionPhaseTimerFactory *metrics.PhaseTimerFactory
}

// NewCryptosimMetrics creates benchmark-specific metrics using the global OTel
// MeterProvider. The caller must configure the MeterProvider before calling
// this. System-level metrics (disk, uptime, process I/O) are handled
// separately via metrics.StartSystemMetrics.
func NewCryptosimMetrics(
	ctx context.Context,
	dbPhaseTimer *metrics.PhaseTimer,
	config *CryptoSimConfig,
) *CryptosimMetrics {

	meter := otel.Meter(cryptosimMeterName)

	blocksFinalizedTotal, _ := meter.Int64Counter(
		"cryptosim_blocks_finalized_total",
		metric.WithDescription("Total number of blocks finalized"),
		metric.WithUnit("{count}"),
	)
	transactionsProcessedTotal, _ := meter.Int64Counter(
		"cryptosim_transactions_processed_total",
		metric.WithDescription("Total number of transactions processed"),
		metric.WithUnit("{count}"),
	)
	totalAccounts, _ := meter.Int64Gauge(
		"cryptosim_accounts_total",
		metric.WithDescription("Total number of accounts"),
		metric.WithUnit("{count}"),
	)
	hotAccounts, _ := meter.Int64Gauge(
		"cryptosim_accounts_hot",
		metric.WithDescription("Number of hot accounts"),
		metric.WithUnit("{count}"),
	)
	coldAccounts, _ := meter.Int64Gauge(
		"cryptosim_accounts_cold",
		metric.WithDescription("Number of cold accounts"),
		metric.WithUnit("{count}"),
	)
	dormantAccounts, _ := meter.Int64Gauge(
		"cryptosim_accounts_dormant",
		metric.WithDescription("Number of dormant accounts"),
		metric.WithUnit("{count}"),
	)
	totalErc20Contracts, _ := meter.Int64Gauge(
		"cryptosim_erc20_contracts_total",
		metric.WithDescription("Total number of ERC20 contracts"),
		metric.WithUnit("{count}"),
	)
	dbCommitsTotal, _ := meter.Int64Counter(
		"cryptosim_db_commits_total",
		metric.WithDescription("Total number of database commits"),
		metric.WithUnit("{count}"),
	)
	receiptBlockWriteDuration, _ := meter.Float64Histogram(
		"cryptosim_receipt_block_write_duration_seconds",
		metric.WithDescription("Time to write a block of receipts to the parquet store"),
		metric.WithExplicitBucketBoundaries(receiptWriteLatencyBuckets...),
		metric.WithUnit("s"),
	)
	receiptChannelDepth, _ := meter.Int64Gauge(
		"cryptosim_receipt_channel_depth",
		metric.WithDescription("Current number of blocks queued for receipt writing"),
		metric.WithUnit("{count}"),
	)
	receiptsWrittenTotal, _ := meter.Int64Counter(
		"cryptosim_receipts_written_total",
		metric.WithDescription("Total number of receipts written to the parquet store"),
		metric.WithUnit("{count}"),
	)
	receiptErrorsTotal, _ := meter.Int64Counter(
		"cryptosim_receipt_errors_total",
		metric.WithDescription("Total receipt processing errors (marshal or write failures)"),
		metric.WithUnit("{count}"),
	)

	mainThreadPhase := dbPhaseTimer
	if mainThreadPhase == nil {
		mainThreadPhase = metrics.NewPhaseTimer(meter, "seidb_main_thread")
	}

	transactionPhaseTimerFactory := metrics.NewPhaseTimerFactory(meter, "transaction")

	m := &CryptosimMetrics{
		ctx:                          ctx,
		blocksFinalizedTotal:         blocksFinalizedTotal,
		transactionsProcessedTotal:   transactionsProcessedTotal,
		totalAccounts:                totalAccounts,
		hotAccounts:                  hotAccounts,
		coldAccounts:                 coldAccounts,
		dormantAccounts:              dormantAccounts,
		totalErc20Contracts:          totalErc20Contracts,
		dbCommitsTotal:               dbCommitsTotal,
		receiptBlockWriteDuration:    receiptBlockWriteDuration,
		receiptChannelDepth:          receiptChannelDepth,
		receiptsWrittenTotal:         receiptsWrittenTotal,
		receiptErrorsTotal:           receiptErrorsTotal,
		mainThreadPhase:              mainThreadPhase,
		transactionPhaseTimerFactory: transactionPhaseTimerFactory,
	}

	return m
}

func (m *CryptosimMetrics) ReportBlockFinalized(transactionCount int64) {
	if m == nil {
		return
	}
	ctx := context.Background()
	if m.blocksFinalizedTotal != nil {
		m.blocksFinalizedTotal.Add(ctx, 1)
	}
	if m.transactionsProcessedTotal != nil {
		m.transactionsProcessedTotal.Add(ctx, transactionCount)
	}
}

func (m *CryptosimMetrics) ReportDBCommit() {
	if m == nil || m.dbCommitsTotal == nil {
		return
	}
	m.dbCommitsTotal.Add(context.Background(), 1)
}

func (m *CryptosimMetrics) SetTotalNumberOfAccounts(total int64, hot int64, cold int64) {
	if m == nil {
		return
	}
	ctx := context.Background()
	if m.totalAccounts != nil {
		m.totalAccounts.Record(ctx, total)
	}
	if m.hotAccounts != nil {
		m.hotAccounts.Record(ctx, hot)
	}
	if m.coldAccounts != nil {
		m.coldAccounts.Record(ctx, cold)
	}
	if m.dormantAccounts != nil {
		m.dormantAccounts.Record(ctx, total-hot-cold)
	}
}

// IncrementTotalNumberOfAccounts updates the account gauges after adding one account.
// Pass the new totals: total, hot, cold. Dormant is derived as total - hot - cold.
func (m *CryptosimMetrics) IncrementTotalNumberOfAccounts(total int64, hot int64, cold int64) {
	if m == nil {
		return
	}
	m.SetTotalNumberOfAccounts(total, hot, cold)
}

func (m *CryptosimMetrics) SetTotalNumberOfERC20Contracts(total int64) {
	if m == nil || m.totalErc20Contracts == nil {
		return
	}
	m.totalErc20Contracts.Record(context.Background(), total)
}

func (m *CryptosimMetrics) GetTransactionPhaseTimerInstance() *metrics.PhaseTimer {
	if m == nil || m.transactionPhaseTimerFactory == nil {
		return nil
	}
	return m.transactionPhaseTimerFactory.Build()
}

func (m *CryptosimMetrics) SetMainThreadPhase(phase string) {
	if m == nil || m.mainThreadPhase == nil {
		return
	}
	m.mainThreadPhase.SetPhase(phase)
}

func (m *CryptosimMetrics) RecordReceiptBlockWriteDuration(latency time.Duration) {
	if m == nil || m.receiptBlockWriteDuration == nil {
		return
	}
	m.receiptBlockWriteDuration.Record(context.Background(), latency.Seconds())
}

func (m *CryptosimMetrics) ReportReceiptsWritten(count int64) {
	if m == nil || m.receiptsWrittenTotal == nil {
		return
	}
	m.receiptsWrittenTotal.Add(context.Background(), count)
}

func (m *CryptosimMetrics) ReportReceiptError() {
	if m == nil || m.receiptErrorsTotal == nil {
		return
	}
	m.receiptErrorsTotal.Add(context.Background(), 1)
}

// startReceiptChannelDepthSampling periodically records the depth of the receipt channel.
func (m *CryptosimMetrics) startReceiptChannelDepthSampling(ch <-chan *block, intervalSeconds int) {
	if m == nil || m.receiptChannelDepth == nil || intervalSeconds <= 0 || ch == nil {
		return
	}
	interval := time.Duration(intervalSeconds) * time.Second
	ctx := context.Background()
	go func() {
		ticker := time.NewTicker(interval)
		defer ticker.Stop()
		for {
			select {
			case <-m.ctx.Done():
				return
			case <-ticker.C:
				m.receiptChannelDepth.Record(ctx, int64(len(ch)))
			}
		}
	}()
}
