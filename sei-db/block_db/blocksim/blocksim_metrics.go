package blocksim

import (
	"context"
	"io/fs"
	"math"
	"path/filepath"
	"time"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/metric"
	"golang.org/x/sys/unix"
)

const blocksimMeterName = "blocksim"

// BlocksimMetrics holds OpenTelemetry metrics for the blocksim benchmark.
type BlocksimMetrics struct {
	ctx context.Context

	blocksWrittenTotal       metric.Int64Counter
	transactionsWrittenTotal metric.Int64Counter
	pruneCallsTotal          metric.Int64Counter
	flushCallsTotal          metric.Int64Counter
	dataDirSizeBytes         metric.Int64Gauge
	dataDirAvailableBytes    metric.Int64Gauge
	uptimeSeconds            metric.Float64Gauge
}

// NewBlocksimMetrics creates metrics for the blocksim benchmark using the
// global OTel MeterProvider. The caller must configure the MeterProvider
// before calling this.
func NewBlocksimMetrics(ctx context.Context, config *BlocksimConfig) *BlocksimMetrics {
	meter := otel.Meter(blocksimMeterName)

	blocksWrittenTotal, _ := meter.Int64Counter(
		"blocksim_blocks_written_total",
		metric.WithDescription("Total number of blocks written to the database"),
		metric.WithUnit("{count}"),
	)
	transactionsWrittenTotal, _ := meter.Int64Counter(
		"blocksim_transactions_written_total",
		metric.WithDescription("Total number of transactions written to the database"),
		metric.WithUnit("{count}"),
	)
	pruneCallsTotal, _ := meter.Int64Counter(
		"blocksim_prune_calls_total",
		metric.WithDescription("Total number of prune calls"),
		metric.WithUnit("{count}"),
	)
	flushCallsTotal, _ := meter.Int64Counter(
		"blocksim_flush_calls_total",
		metric.WithDescription("Total number of flush calls"),
		metric.WithUnit("{count}"),
	)
	dataDirSizeBytes, _ := meter.Int64Gauge(
		"blocksim_data_dir_size_bytes",
		metric.WithDescription("Approximate size in bytes of the benchmark data directory"),
		metric.WithUnit("By"),
	)
	dataDirAvailableBytes, _ := meter.Int64Gauge(
		"blocksim_data_dir_available_bytes",
		metric.WithDescription("Available disk space in bytes on the filesystem containing the data directory"),
		metric.WithUnit("By"),
	)
	uptimeSeconds, _ := meter.Float64Gauge(
		"blocksim_uptime_seconds",
		metric.WithDescription("Seconds since benchmark started"),
		metric.WithUnit("s"),
	)

	m := &BlocksimMetrics{
		ctx:                      ctx,
		blocksWrittenTotal:       blocksWrittenTotal,
		transactionsWrittenTotal: transactionsWrittenTotal,
		pruneCallsTotal:          pruneCallsTotal,
		flushCallsTotal:          flushCallsTotal,
		dataDirSizeBytes:         dataDirSizeBytes,
		dataDirAvailableBytes:    dataDirAvailableBytes,
		uptimeSeconds:            uptimeSeconds,
	}

	if config.BackgroundMetricsScrapeInterval > 0 {
		m.startDirSizeSampling(config.DataDir, m.dataDirSizeBytes, config.BackgroundMetricsScrapeInterval)
		m.startAvailableDiskSpaceSampling(config.DataDir, config.BackgroundMetricsScrapeInterval)
		m.startUptimeSampling(time.Now())
	}

	return m
}

func (m *BlocksimMetrics) ReportBlockWritten(transactionCount int64) {
	if m == nil {
		return
	}
	ctx := context.Background()
	if m.blocksWrittenTotal != nil {
		m.blocksWrittenTotal.Add(ctx, 1)
	}
	if m.transactionsWrittenTotal != nil {
		m.transactionsWrittenTotal.Add(ctx, transactionCount)
	}
}

func (m *BlocksimMetrics) ReportPrune() {
	if m == nil || m.pruneCallsTotal == nil {
		return
	}
	m.pruneCallsTotal.Add(context.Background(), 1)
}

func (m *BlocksimMetrics) ReportFlush() {
	if m == nil || m.flushCallsTotal == nil {
		return
	}
	m.flushCallsTotal.Add(context.Background(), 1)
}

func (m *BlocksimMetrics) startUptimeSampling(startTime time.Time) {
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

func (m *BlocksimMetrics) startPeriodicSampling(intervalSeconds int, sampleFn func()) {
	if m == nil || intervalSeconds <= 0 || sampleFn == nil {
		return
	}
	interval := time.Duration(intervalSeconds) * time.Second
	go func() {
		ticker := time.NewTicker(interval)
		defer ticker.Stop()
		sampleFn()
		for {
			select {
			case <-m.ctx.Done():
				return
			case <-ticker.C:
				sampleFn()
			}
		}
	}()
}

func (m *BlocksimMetrics) startDirSizeSampling(dir string, gauge metric.Int64Gauge, intervalSeconds int) {
	if dir == "" || gauge == nil {
		return
	}
	ctx := context.Background()
	m.startPeriodicSampling(intervalSeconds, func() {
		gauge.Record(ctx, measureDirSize(dir))
	})
}

func (m *BlocksimMetrics) startAvailableDiskSpaceSampling(dir string, intervalSeconds int) {
	if dir == "" || m.dataDirAvailableBytes == nil {
		return
	}
	ctx := context.Background()
	m.startPeriodicSampling(intervalSeconds, func() {
		m.dataDirAvailableBytes.Record(ctx, measureAvailableBytes(dir))
	})
}

func measureDirSize(dir string) int64 {
	var total int64
	_ = filepath.WalkDir(dir, func(_ string, entry fs.DirEntry, err error) error {
		if err != nil || entry.IsDir() {
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

func measureAvailableBytes(dir string) int64 {
	var stat unix.Statfs_t
	if err := unix.Statfs(dir, &stat); err != nil {
		return 0
	}
	result := stat.Bavail * uint64(stat.Bsize) //nolint:gosec
	if result > math.MaxInt64 {
		return math.MaxInt64
	}
	return int64(result)
}
