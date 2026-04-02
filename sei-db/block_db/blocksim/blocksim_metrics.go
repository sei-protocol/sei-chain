package blocksim

import (
	"context"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/metric"
)

const blocksimMeterName = "blocksim"

// BlocksimMetrics holds OpenTelemetry metrics for the blocksim benchmark.
type BlocksimMetrics struct {
	ctx context.Context

	blocksWrittenTotal       metric.Int64Counter
	transactionsWrittenTotal metric.Int64Counter
	bytesWrittenTotal        metric.Int64Counter
	pruneCallsTotal          metric.Int64Counter
	flushCallsTotal          metric.Int64Counter
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
	bytesWrittenTotal, _ := meter.Int64Counter(
		"blocksim_bytes_written_total",
		metric.WithDescription("Total bytes of block and transaction data written to the database"),
		metric.WithUnit("By"),
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
	return &BlocksimMetrics{
		ctx:                      ctx,
		blocksWrittenTotal:       blocksWrittenTotal,
		transactionsWrittenTotal: transactionsWrittenTotal,
		bytesWrittenTotal:        bytesWrittenTotal,
		pruneCallsTotal:          pruneCallsTotal,
		flushCallsTotal:          flushCallsTotal,
	}
}

func (m *BlocksimMetrics) ReportBlockWritten(transactionCount int64, byteCount int64) {
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
	if m.bytesWrittenTotal != nil {
		m.bytesWrittenTotal.Add(ctx, byteCount)
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
