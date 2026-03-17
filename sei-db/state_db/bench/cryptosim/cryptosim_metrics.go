package cryptosim

import (
	"context"
	"io/fs"
	"math"
	"os"
	"path/filepath"
	"runtime"
	"time"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/metric"
	"golang.org/x/sys/unix"

	"github.com/sei-protocol/sei-chain/sei-db/common/metrics"
	"github.com/shirou/gopsutil/v3/process"
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
	dataDirSizeBytes           metric.Int64Gauge
	dataDirAvailableBytes      metric.Int64Gauge
	processReadBytesTotal      metric.Int64Counter
	processWriteBytesTotal     metric.Int64Counter
	processReadCountTotal      metric.Int64Counter
	processWriteCountTotal     metric.Int64Counter
	uptimeSeconds              metric.Float64Gauge

	// Receipt metrics
	receiptBlockWriteDuration metric.Float64Histogram
	receiptChannelDepth       metric.Int64Gauge
	receiptsWrittenTotal      metric.Int64Counter
	receiptErrorsTotal        metric.Int64Counter
	receiptReadDuration       metric.Float64Histogram
	receiptReadsTotal         metric.Int64Counter
	receiptCacheHitsTotal     metric.Int64Counter
	receiptCacheMissesTotal   metric.Int64Counter
	receiptLogFilterDuration  metric.Float64Histogram

	mainThreadPhase              *metrics.PhaseTimer
	transactionPhaseTimerFactory *metrics.PhaseTimerFactory
}

// NewCryptosimMetrics creates metrics for the cryptosim benchmark using the
// global OTel MeterProvider. The caller (e.g., main) must configure the
// MeterProvider with a Prometheus or other exporter before calling this.
// When ctx is cancelled, background sampling goroutines exit.
// Data directory size sampling is started automatically when
// BackgroundMetricsScrapeInterval > 0.
//
// Unit convention: Use WithUnit values from the UCUM standard (see
// https://ucum.org/ucum). Durations use "s" (seconds). Bytes use "By".
// Counts use curly-brace annotations, e.g. "{count}" for generic counts or
// more specific "{block}", "{transaction}", "{account}" to match what is measured.
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
	dataDirSizeBytes, _ := meter.Int64Gauge(
		"cryptosim_data_dir_size_bytes",
		metric.WithDescription("Approximate size in bytes of the benchmark data directory"),
		metric.WithUnit("By"),
	)
	dataDirAvailableBytes, _ := meter.Int64Gauge(
		"cryptosim_data_dir_available_bytes",
		metric.WithDescription("Available disk space in bytes on the filesystem containing the data directory"),
		metric.WithUnit("By"),
	)
	processReadBytesTotal, _ := meter.Int64Counter(
		"cryptosim_process_read_bytes_total",
		metric.WithDescription("Bytes read from storage by benchmark. Use rate() for throughput. Linux only."),
		metric.WithUnit("By"),
	)
	processWriteBytesTotal, _ := meter.Int64Counter(
		"cryptosim_process_write_bytes_total",
		metric.WithDescription("Bytes written to storage by benchmark. Use rate() for throughput. Linux only."),
		metric.WithUnit("By"),
	)
	processReadCountTotal, _ := meter.Int64Counter(
		"cryptosim_process_read_count_total",
		metric.WithDescription("Read I/O ops by benchmark. Use rate() for read IOPS. Linux only."),
		metric.WithUnit("{count}"),
	)
	processWriteCountTotal, _ := meter.Int64Counter(
		"cryptosim_process_write_count_total",
		metric.WithDescription("Write I/O ops by benchmark. Use rate() for write IOPS. Linux only."),
		metric.WithUnit("{count}"),
	)
	uptimeSeconds, _ := meter.Float64Gauge(
		"cryptosim_uptime_seconds",
		metric.WithDescription("Seconds since benchmark started. Resets to 0 on restart."),
		metric.WithUnit("s"),
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
	receiptReadDuration, _ := meter.Float64Histogram(
		"cryptosim_receipt_read_duration_seconds",
		metric.WithDescription("DuckDB receipt read latency (cache misses only)"),
		metric.WithUnit("s"),
	)
	receiptReadsTotal, _ := meter.Int64Counter(
		"cryptosim_receipt_reads_total",
		metric.WithDescription("Total receipt read attempts"),
		metric.WithUnit("{count}"),
	)
	receiptCacheHitsTotal, _ := meter.Int64Counter(
		"cryptosim_receipt_cache_hits_total",
		metric.WithDescription("Receipt reads served from the ledger cache"),
		metric.WithUnit("{count}"),
	)
	receiptCacheMissesTotal, _ := meter.Int64Counter(
		"cryptosim_receipt_cache_misses_total",
		metric.WithDescription("Receipt reads that went to DuckDB"),
		metric.WithUnit("{count}"),
	)
	receiptLogFilterDuration, _ := meter.Float64Histogram(
		"cryptosim_receipt_log_filter_duration_seconds",
		metric.WithDescription("DuckDB eth_getLogs filter query latency"),
		metric.WithUnit("s"),
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
		dataDirSizeBytes:             dataDirSizeBytes,
		dataDirAvailableBytes:        dataDirAvailableBytes,
		processReadBytesTotal:        processReadBytesTotal,
		processWriteBytesTotal:       processWriteBytesTotal,
		processReadCountTotal:        processReadCountTotal,
		processWriteCountTotal:       processWriteCountTotal,
		uptimeSeconds:                uptimeSeconds,
		receiptBlockWriteDuration:    receiptBlockWriteDuration,
		receiptChannelDepth:          receiptChannelDepth,
		receiptsWrittenTotal:         receiptsWrittenTotal,
		receiptErrorsTotal:           receiptErrorsTotal,
		receiptReadDuration:          receiptReadDuration,
		receiptReadsTotal:            receiptReadsTotal,
		receiptCacheHitsTotal:        receiptCacheHitsTotal,
		receiptCacheMissesTotal:      receiptCacheMissesTotal,
		receiptLogFilterDuration:     receiptLogFilterDuration,
		mainThreadPhase:              mainThreadPhase,
		transactionPhaseTimerFactory: transactionPhaseTimerFactory,
	}
	if config != nil && config.BackgroundMetricsScrapeInterval > 0 && config.DataDir != "" {
		if dataDir, err := resolveAndCreateDataDir(config.DataDir); err == nil {
			m.startDataDirSizeSampling(dataDir, config.BackgroundMetricsScrapeInterval)
		}
		m.startProcessIOSampling(config.BackgroundMetricsScrapeInterval)
		m.startUptimeSampling(time.Now())
	}
	return m
}

func (m *CryptosimMetrics) startUptimeSampling(startTime time.Time) {
	if m == nil || m.uptimeSeconds == nil {
		return
	}
	go func() {
		ticker := time.NewTicker(time.Second)
		defer ticker.Stop()
		ctx := context.Background()
		for {
			select {
			case <-m.ctx.Done():
				return
			case <-ticker.C:
				m.uptimeSeconds.Record(ctx, time.Since(startTime).Seconds())
			}
		}
	}()
}

// startProcessIOSampling starts a goroutine that periodically samples process
// I/O counters (read/write bytes and operation counts) via gopsutil and adds
// deltas to OTel counters. Use rate() on these counters for throughput and IOPS.
// Skipped on darwin: gopsutil does not implement process.IOCounters on macOS.
func (m *CryptosimMetrics) startProcessIOSampling(intervalSeconds int) {
	if m == nil || intervalSeconds <= 0 {
		return
	}
	if runtime.GOOS == "darwin" {
		return
	}
	pid := os.Getpid()
	if pid < 0 || pid > math.MaxInt32 {
		return
	}
	proc, err := process.NewProcess(int32(pid))
	if err != nil {
		return
	}
	interval := time.Duration(intervalSeconds) * time.Second
	var prevReadBytes, prevWriteBytes, prevReadCount, prevWriteCount uint64
	var initialized bool
	ctx := context.Background()
	go func() {
		ticker := time.NewTicker(interval)
		defer ticker.Stop()
		sample := func() {
			io, err := proc.IOCounters()
			if err != nil || io == nil {
				return
			}
			curRead := io.ReadBytes
			curWrite := io.WriteBytes
			curReadCount := io.ReadCount
			curWriteCount := io.WriteCount
			if initialized {
				if curRead >= prevReadBytes && m.processReadBytesTotal != nil {
					delta := curRead - prevReadBytes
					m.processReadBytesTotal.Add(ctx, uint64ToInt64Clamped(delta))
				}
				if curWrite >= prevWriteBytes && m.processWriteBytesTotal != nil {
					delta := curWrite - prevWriteBytes
					m.processWriteBytesTotal.Add(ctx, uint64ToInt64Clamped(delta))
				}
				if curReadCount >= prevReadCount && m.processReadCountTotal != nil {
					delta := curReadCount - prevReadCount
					m.processReadCountTotal.Add(ctx, uint64ToInt64Clamped(delta))
				}
				if curWriteCount >= prevWriteCount && m.processWriteCountTotal != nil {
					delta := curWriteCount - prevWriteCount
					m.processWriteCountTotal.Add(ctx, uint64ToInt64Clamped(delta))
				}
			}
			prevReadBytes, prevWriteBytes = curRead, curWrite
			prevReadCount, prevWriteCount = curReadCount, curWriteCount
			initialized = true
		}
		sample()
		for {
			select {
			case <-m.ctx.Done():
				return
			case <-ticker.C:
				sample()
			}
		}
	}()
}

func (m *CryptosimMetrics) startDataDirSizeSampling(dataDir string, intervalSeconds int) {
	if m == nil || intervalSeconds <= 0 || dataDir == "" {
		return
	}
	interval := time.Duration(intervalSeconds) * time.Second
	ctx := context.Background()
	go func() {
		ticker := time.NewTicker(interval)
		defer ticker.Stop()
		sample := func() {
			if m.dataDirSizeBytes != nil {
				m.dataDirSizeBytes.Record(ctx, measureDataDirSize(dataDir))
			}
			if m.dataDirAvailableBytes != nil {
				m.dataDirAvailableBytes.Record(ctx, measureDataDirAvailableBytes(dataDir))
			}
		}
		sample()
		for {
			select {
			case <-m.ctx.Done():
				return
			case <-ticker.C:
				sample()
			}
		}
	}()
}

// uint64ToInt64Clamped converts a uint64 to int64, clamping to math.MaxInt64 to avoid overflow.
func uint64ToInt64Clamped(v uint64) int64 {
	if v > math.MaxInt64 {
		return math.MaxInt64
	}
	return int64(v)
}

func measureDataDirAvailableBytes(dataDir string) int64 {
	var stat unix.Statfs_t
	if err := unix.Statfs(dataDir, &stat); err != nil {
		return 0
	}
	result := stat.Bavail * uint64(stat.Bsize) //nolint:gosec
	if result > math.MaxInt64 {
		return math.MaxInt64
	}
	return int64(result)
}

func measureDataDirSize(dataDir string) int64 {
	var total int64
	_ = filepath.WalkDir(dataDir, func(path string, entry fs.DirEntry, err error) error {
		if err != nil {
			return nil
		}
		if entry.IsDir() {
			return nil
		}
		info, err := entry.Info()
		if err != nil {
			return nil
		}
		total += info.Size()
		return nil
	})
	return total
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

func (m *CryptosimMetrics) RecordReceiptBlockWriteDuration(seconds float64) {
	if m == nil || m.receiptBlockWriteDuration == nil {
		return
	}
	m.receiptBlockWriteDuration.Record(context.Background(), seconds)
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

func (m *CryptosimMetrics) RecordReceiptReadDuration(seconds float64) {
	if m == nil || m.receiptReadDuration == nil {
		return
	}
	m.receiptReadDuration.Record(context.Background(), seconds)
}

func (m *CryptosimMetrics) ReportReceiptRead() {
	if m == nil || m.receiptReadsTotal == nil {
		return
	}
	m.receiptReadsTotal.Add(context.Background(), 1)
}

func (m *CryptosimMetrics) ReportReceiptCacheHit() {
	if m == nil || m.receiptCacheHitsTotal == nil {
		return
	}
	m.receiptCacheHitsTotal.Add(context.Background(), 1)
}

func (m *CryptosimMetrics) ReportReceiptCacheMiss() {
	if m == nil || m.receiptCacheMissesTotal == nil {
		return
	}
	m.receiptCacheMissesTotal.Add(context.Background(), 1)
}

func (m *CryptosimMetrics) RecordReceiptLogFilterDuration(seconds float64) {
	if m == nil || m.receiptLogFilterDuration == nil {
		return
	}
	m.receiptLogFilterDuration.Record(context.Background(), seconds)
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
