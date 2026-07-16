package blocksim

import (
	"context"

	"github.com/sei-protocol/sei-chain/sei-db/common/metrics"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/metric"
)

const blocksimMeterName = "blocksim"

// BlocksimMetrics holds OpenTelemetry metrics for the blocksim benchmark.
type BlocksimMetrics struct {
	ctx context.Context

	blocksWrittenTotal       metric.Int64Counter
	transactionsWrittenTotal metric.Int64Counter
	qcsWrittenTotal          metric.Int64Counter
	bytesWrittenTotal        metric.Int64Counter
	pruneCallsTotal          metric.Int64Counter
	flushCallsTotal          metric.Int64Counter

	lowestBlockHeight  metric.Int64Gauge
	highestBlockHeight metric.Int64Gauge
	blockSizeBytes     metric.Int64Gauge

	mainThreadPhase *metrics.PhaseTimer
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
		metric.WithDescription("Total number of transactions written to the database (summed across all block payloads)"),
		metric.WithUnit("{count}"),
	)
	qcsWrittenTotal, _ := meter.Int64Counter(
		"blocksim_qcs_written_total",
		metric.WithDescription("Total number of commit QCs written to the database"),
		metric.WithUnit("{count}"),
	)
	bytesWrittenTotal, _ := meter.Int64Counter(
		"blocksim_bytes_written_total",
		metric.WithDescription("Total bytes of block payload data written to the database"),
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

	lowestBlockHeight, _ := meter.Int64Gauge(
		"blocksim_lowest_block_height",
		metric.WithDescription("Lowest block height currently stored in the database"),
		metric.WithUnit("{height}"),
	)
	highestBlockHeight, _ := meter.Int64Gauge(
		"blocksim_highest_block_height",
		metric.WithDescription("Highest block height currently stored in the database"),
		metric.WithUnit("{height}"),
	)
	blockSizeBytes, _ := meter.Int64Gauge(
		"blocksim_block_size_bytes",
		metric.WithDescription("Size in bytes of a single generated block (constant for a given config)"),
		metric.WithUnit("By"),
	)

	mainThreadPhase := metrics.NewPhaseTimer(meter, "blocksim_main_thread")

	m := &BlocksimMetrics{
		ctx:                      ctx,
		blocksWrittenTotal:       blocksWrittenTotal,
		transactionsWrittenTotal: transactionsWrittenTotal,
		qcsWrittenTotal:          qcsWrittenTotal,
		bytesWrittenTotal:        bytesWrittenTotal,
		pruneCallsTotal:          pruneCallsTotal,
		flushCallsTotal:          flushCallsTotal,
		lowestBlockHeight:        lowestBlockHeight,
		highestBlockHeight:       highestBlockHeight,
		blockSizeBytes:           blockSizeBytes,
		mainThreadPhase:          mainThreadPhase,
	}

	m.recordBlockSize(config)
	return m
}

func (m *BlocksimMetrics) recordBlockSize(config *BlocksimConfig) {
	if m == nil || m.blockSizeBytes == nil {
		return
	}
	size := int64(config.TransactionsPerBlock * config.BytesPerTransaction) //nolint:gosec
	m.blockSizeBytes.Record(context.Background(), size)
}

// RecordLowestHeight records the lowest retained block height as a gauge.
func (m *BlocksimMetrics) RecordLowestHeight(height uint64) {
	if m == nil || m.lowestBlockHeight == nil {
		return
	}
	m.lowestBlockHeight.Record(context.Background(), int64(height)) //nolint:gosec
}

// RecordHighestHeight records the highest written block height as a gauge.
func (m *BlocksimMetrics) RecordHighestHeight(height uint64) {
	if m == nil || m.highestBlockHeight == nil {
		return
	}
	m.highestBlockHeight.Record(context.Background(), int64(height)) //nolint:gosec
}

func (m *BlocksimMetrics) ReportBlockWritten(byteCount int64, txCount int64) {
	if m == nil {
		return
	}
	ctx := context.Background()
	if m.blocksWrittenTotal != nil {
		m.blocksWrittenTotal.Add(ctx, 1)
	}
	if m.transactionsWrittenTotal != nil {
		m.transactionsWrittenTotal.Add(ctx, txCount)
	}
	if m.bytesWrittenTotal != nil {
		m.bytesWrittenTotal.Add(ctx, byteCount)
	}
}

func (m *BlocksimMetrics) ReportQCWritten() {
	if m == nil || m.qcsWrittenTotal == nil {
		return
	}
	m.qcsWrittenTotal.Add(context.Background(), 1)
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

func (m *BlocksimMetrics) SetMainThreadPhase(phase string) {
	if m == nil || m.mainThreadPhase == nil {
		return
	}
	m.mainThreadPhase.SetPhase(phase)
}
