package blocksim

import (
	"context"
	"time"

	"github.com/sei-protocol/sei-chain/sei-db/common/metrics"
	"github.com/sei-protocol/sei-chain/sei-db/ledger_db/block"
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
		metric.WithDescription("Total number of transactions written to the database"),
		metric.WithUnit("{count}"),
	)
	bytesWrittenTotal, _ := meter.Int64Counter(
		"blocksim_bytes_written_total",
		metric.WithDescription("Total bytes of block and transaction data written to the database"),
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
	size := int64(config.BlockHashSize + config.ExtraBytesPerBlock + //nolint:gosec
		config.TransactionsPerBlock*(config.TransactionHashSize+config.BytesPerTransaction))
	m.blockSizeBytes.Record(context.Background(), size)
}

// StartBlockDBPolling launches a background goroutine that periodically queries
// the database for the current lowest and highest block heights, recording them
// as gauge metrics. The goroutine exits when ctx is cancelled.
func (m *BlocksimMetrics) StartBlockDBPolling(ctx context.Context, db block.BlockDB, intervalSeconds int) {
	if m == nil || intervalSeconds <= 0 {
		return
	}
	interval := time.Duration(intervalSeconds) * time.Second
	go func() {
		ticker := time.NewTicker(interval)
		defer ticker.Stop()
		m.pollBlockHeights(ctx, db)
		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				m.pollBlockHeights(ctx, db)
			}
		}
	}()
}

func (m *BlocksimMetrics) pollBlockHeights(ctx context.Context, db block.BlockDB) {
	bg := context.Background()
	if lo, err := db.GetLowestBlockHeight(ctx); err == nil {
		m.lowestBlockHeight.Record(bg, int64(lo)) //nolint:gosec
	}
	if hi, err := db.GetHighestBlockHeight(ctx); err == nil {
		m.highestBlockHeight.Record(bg, int64(hi)) //nolint:gosec
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

func (m *BlocksimMetrics) SetMainThreadPhase(phase string) {
	if m == nil || m.mainThreadPhase == nil {
		return
	}
	m.mainThreadPhase.SetPhase(phase)
}
