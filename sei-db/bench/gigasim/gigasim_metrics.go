package gigasim

import (
	"context"
	"time"

	"github.com/sei-protocol/sei-chain/sei-db/common/metrics"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/metric"
)

// gigasimMeterName is the OTel meter every gigasim instrument is created on.
const gigasimMeterName = "gigasim"

// The stores write volume is attributed to, as the store label on gigasim_store_bytes_written_total.
// Each names the directory the store occupies under the data directory.
const (
	storeBlockDB     = "block_db"
	storeReceiptDB   = "receipt_db"
	storeStateCommit = "state_commit"
	storeStateStore  = "state_store"
)

// GigasimMetrics holds the OpenTelemetry instruments for the gigasim benchmark.
//
// Every method tolerates a nil receiver and a nil instrument, so a benchmark can run without a
// configured meter provider.
type GigasimMetrics struct {
	blocksProcessedTotal      metric.Int64Counter
	transactionsExecutedTotal metric.Int64Counter
	storeBytesWrittenTotal    metric.Int64Counter
	qcsWrittenTotal           metric.Int64Counter
	stateCommitsTotal         metric.Int64Counter
	stateChangesTotal         metric.Int64Counter
	receiptsWrittenTotal      metric.Int64Counter
	flushCallsTotal           metric.Int64Counter

	highestBlockHeight  metric.Int64Gauge
	totalAccounts       metric.Int64Gauge
	hotAccounts         metric.Int64Gauge
	coldAccounts        metric.Int64Gauge
	dormantAccounts     metric.Int64Gauge
	erc20Contracts      metric.Int64Gauge
	stagedBlockQueueLen metric.Int64Gauge

	blockHashWaitSeconds metric.Float64Histogram

	mainThreadPhase   *metrics.PhaseTimer
	generatorPhase    *metrics.PhaseTimer
	transactionPhases *metrics.PhaseTimerFactory
	lifecyclePhases   *metrics.PhaseTimerFactory
}

// NewGigasimMetrics creates the benchmark's instruments on the global OTel MeterProvider, which the
// caller must configure first.
func NewGigasimMetrics() *GigasimMetrics {
	meter := otel.Meter(gigasimMeterName)

	blocksProcessedTotal, _ := meter.Int64Counter(
		"gigasim_blocks_processed_total",
		metric.WithDescription("Total number of blocks taken through the full storage pipeline"),
		metric.WithUnit("{count}"),
	)
	transactionsExecutedTotal, _ := meter.Int64Counter(
		"gigasim_transactions_executed_total",
		metric.WithDescription("Total number of simulated transactions executed against the state DB"),
		metric.WithUnit("{count}"),
	)
	storeBytesWrittenTotal, _ := meter.Int64Counter(
		"gigasim_store_bytes_written_total",
		metric.WithDescription("Total bytes handed to each store, labelled by store"),
		metric.WithUnit("By"),
	)
	qcsWrittenTotal, _ := meter.Int64Counter(
		"gigasim_qcs_written_total",
		metric.WithDescription("Total number of commit QCs written to the block store"),
		metric.WithUnit("{count}"),
	)
	stateCommitsTotal, _ := meter.Int64Counter(
		"gigasim_state_commits_total",
		metric.WithDescription("Total number of blocks committed to the state DB"),
		metric.WithUnit("{count}"),
	)
	stateChangesTotal, _ := meter.Int64Counter(
		"gigasim_state_changes_total",
		metric.WithDescription("Total number of key-value changes committed to the state DB"),
		metric.WithUnit("{count}"),
	)
	receiptsWrittenTotal, _ := meter.Int64Counter(
		"gigasim_receipts_written_total",
		metric.WithDescription("Total number of receipts written to the receipt store"),
		metric.WithUnit("{count}"),
	)
	flushCallsTotal, _ := meter.Int64Counter(
		"gigasim_flush_calls_total",
		metric.WithDescription("Total number of block store flush calls"),
		metric.WithUnit("{count}"),
	)

	highestBlockHeight, _ := meter.Int64Gauge(
		"gigasim_highest_block_height",
		metric.WithDescription("Highest block height taken through the pipeline"),
		metric.WithUnit("{height}"),
	)
	totalAccounts, _ := meter.Int64Gauge(
		"gigasim_accounts_total",
		metric.WithDescription("Number of accounts in existence"),
		metric.WithUnit("{count}"),
	)
	hotAccounts, _ := meter.Int64Gauge(
		"gigasim_accounts_hot",
		metric.WithDescription("Number of accounts in the hot set"),
		metric.WithUnit("{count}"),
	)
	coldAccounts, _ := meter.Int64Gauge(
		"gigasim_accounts_cold",
		metric.WithDescription("Number of accounts eligible for cold selection"),
		metric.WithUnit("{count}"),
	)
	dormantAccounts, _ := meter.Int64Gauge(
		"gigasim_accounts_dormant",
		metric.WithDescription("Number of accounts that exist but are never selected"),
		metric.WithUnit("{count}"),
	)
	erc20Contracts, _ := meter.Int64Gauge(
		"gigasim_erc20_contracts_total",
		metric.WithDescription("Number of simulated ERC20 contracts in existence"),
		metric.WithUnit("{count}"),
	)
	stagedBlockQueueLen, _ := meter.Int64Gauge(
		"gigasim_staged_block_queue_depth",
		metric.WithDescription("Number of generated blocks waiting to enter the pipeline"),
		metric.WithUnit("{count}"),
	)

	blockHashWaitSeconds, _ := meter.Float64Histogram(
		"gigasim_block_hash_wait_seconds",
		metric.WithDescription("Time the main thread spent waiting for a block hash"),
		metric.WithUnit("s"),
		metric.WithExplicitBucketBoundaries(metrics.LatencyBuckets...),
	)

	return &GigasimMetrics{
		blocksProcessedTotal:      blocksProcessedTotal,
		transactionsExecutedTotal: transactionsExecutedTotal,
		storeBytesWrittenTotal:    storeBytesWrittenTotal,
		qcsWrittenTotal:           qcsWrittenTotal,
		stateCommitsTotal:         stateCommitsTotal,
		stateChangesTotal:         stateChangesTotal,
		receiptsWrittenTotal:      receiptsWrittenTotal,
		flushCallsTotal:           flushCallsTotal,
		highestBlockHeight:        highestBlockHeight,
		totalAccounts:             totalAccounts,
		hotAccounts:               hotAccounts,
		coldAccounts:              coldAccounts,
		dormantAccounts:           dormantAccounts,
		erc20Contracts:            erc20Contracts,
		stagedBlockQueueLen:       stagedBlockQueueLen,
		blockHashWaitSeconds:      blockHashWaitSeconds,
		mainThreadPhase:           metrics.NewPhaseTimer(meter, "gigasim_main_thread"),
		generatorPhase:            metrics.NewPhaseTimer(meter, "gigasim_generator"),
		transactionPhases:         metrics.NewPhaseTimerFactory(meter, "gigasim_transaction"),
		lifecyclePhases:           metrics.NewPhaseTimerFactory(meter, "gigasim_lifecycle"),
	}
}

// NewLifecycleTimer returns a timer recording one goroutine's share of a block's critical path.
//
// A block is produced on the generator's goroutine and consumed on the main loop's, so the phases that
// make up its latency are spread over both and each needs a timer of its own. They publish to one set
// of instruments, which is what lets a single query stack the whole path.
//
// The phases recorded here are only the work a block passes through. Time a goroutine spends waiting
// for the other one is left out, so that the phases sum to the latency of a block rather than to the
// wall clock of two goroutines. The commit itself is left to the state DB, which is the layer that can
// tell the state WAL, SC and SS apart.
func (m *GigasimMetrics) NewLifecycleTimer() *metrics.PhaseTimer {
	if m == nil || m.lifecyclePhases == nil {
		return nil
	}
	return m.lifecyclePhases.Build()
}

// NewTransactionPhaseTimer returns a phase timer for one executor. Each executor needs its own: a
// timer tracks a single thread's current phase.
func (m *GigasimMetrics) NewTransactionPhaseTimer() *metrics.PhaseTimer {
	if m == nil || m.transactionPhases == nil {
		return nil
	}
	return m.transactionPhases.Build()
}

// SetMainThreadPhase records the main thread moving into a new stage of the pipeline.
func (m *GigasimMetrics) SetMainThreadPhase(phase string) {
	if m == nil {
		return
	}
	m.mainThreadPhase.SetPhase(phase)
}

// SetGeneratorPhase records the generator thread moving into a new stage of block production.
func (m *GigasimMetrics) SetGeneratorPhase(phase string) {
	if m == nil {
		return
	}
	m.generatorPhase.SetPhase(phase)
}

// ReportBlockProcessed records one block completing every stage of the pipeline.
func (m *GigasimMetrics) ReportBlockProcessed(number int64, transactions int64) {
	if m == nil {
		return
	}
	ctx := context.Background()
	if m.blocksProcessedTotal != nil {
		m.blocksProcessedTotal.Add(ctx, 1)
	}
	if m.transactionsExecutedTotal != nil {
		m.transactionsExecutedTotal.Add(ctx, transactions)
	}
	if m.highestBlockHeight != nil {
		m.highestBlockHeight.Record(ctx, number)
	}
}

// ReportStoreBytesWritten records bytes handed to one store, named by a store constant. It is the
// volume the benchmark asked the store to take, so it excludes whatever the engine below amplifies it
// to; the pebble_ and litt_ instruments carry that.
func (m *GigasimMetrics) ReportStoreBytesWritten(store string, bytes int64) {
	if m == nil || m.storeBytesWrittenTotal == nil {
		return
	}
	m.storeBytesWrittenTotal.Add(context.Background(), bytes,
		metric.WithAttributes(attribute.String("store", store)))
}

// ReportQCWritten records one commit QC reaching the block store.
func (m *GigasimMetrics) ReportQCWritten() {
	if m == nil || m.qcsWrittenTotal == nil {
		return
	}
	m.qcsWrittenTotal.Add(context.Background(), 1)
}

// ReportStateCommit records one block committing to the state DB, and the changes it carried.
func (m *GigasimMetrics) ReportStateCommit(changes int64) {
	if m == nil {
		return
	}
	ctx := context.Background()
	if m.stateCommitsTotal != nil {
		m.stateCommitsTotal.Add(ctx, 1)
	}
	if m.stateChangesTotal != nil {
		m.stateChangesTotal.Add(ctx, changes)
	}
}

// ReportReceiptsWritten records a block's receipts reaching the receipt store.
func (m *GigasimMetrics) ReportReceiptsWritten(count int64) {
	if m == nil || m.receiptsWrittenTotal == nil {
		return
	}
	m.receiptsWrittenTotal.Add(context.Background(), count)
}

// ReportFlush records one block store flush.
func (m *GigasimMetrics) ReportFlush() {
	if m == nil || m.flushCallsTotal == nil {
		return
	}
	m.flushCallsTotal.Add(context.Background(), 1)
}

// RecordBlockHashWaitDuration records how long the main thread waited for one block's hash.
func (m *GigasimMetrics) RecordBlockHashWaitDuration(d time.Duration) {
	if m == nil || m.blockHashWaitSeconds == nil {
		return
	}
	m.blockHashWaitSeconds.Record(context.Background(), d.Seconds())
}

// SetAccountCounts records the size of the account population and of the sets it divides into.
func (m *GigasimMetrics) SetAccountCounts(total int64, hot int64, cold int64, dormant int64) {
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
		m.dormantAccounts.Record(ctx, dormant)
	}
}

// SetErc20ContractCount records how many simulated ERC20 contracts exist.
func (m *GigasimMetrics) SetErc20ContractCount(count int64) {
	if m == nil || m.erc20Contracts == nil {
		return
	}
	m.erc20Contracts.Record(context.Background(), count)
}

// RecordStagedBlockQueueDepth records how many generated blocks are waiting to be processed, which
// shows whether generation or storage is the limit.
func (m *GigasimMetrics) RecordStagedBlockQueueDepth(depth int64) {
	if m == nil || m.stagedBlockQueueLen == nil {
		return
	}
	m.stagedBlockQueueLen.Record(context.Background(), depth)
}
