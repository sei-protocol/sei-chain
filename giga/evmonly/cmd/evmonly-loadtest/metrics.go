package main

import (
	"context"
	"errors"
	"fmt"
	"net"
	"net/http"
	"sync/atomic"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promhttp"
	"github.com/sei-protocol/sei-chain/giga/evmonly"
)

type loadMetrics struct {
	inputBlocks     atomic.Uint64
	preparedBlocks  atomic.Uint64
	preparedTxs     atomic.Uint64
	finishedBlocks  atomic.Uint64
	finishedTxs     atomic.Uint64
	successfulTxs   atomic.Uint64
	failedTxs       atomic.Uint64
	gasConsumed     atomic.Uint64
	prepareErrors   atomic.Uint64
	executionErrors atomic.Uint64
	occAttempts     atomic.Uint64
	occFallbacks    atomic.Uint64
	occReruns       atomic.Uint64
	occConflicts    atomic.Uint64
	sinkEnqueued    atomic.Uint64
	sinkWritten     atomic.Uint64
	sinkBytes       atomic.Uint64
	sinkWaitNanos   atomic.Uint64
	sinkWaitEvents  atomic.Uint64
	sinkWriteNanos  atomic.Uint64
	sinkQueued      atomic.Int64
	poolCapacity    atomic.Int64
	poolAvailable   atomic.Int64
	poolOverflow    atomic.Uint64

	inputBlocksTotal     prometheus.Counter
	preparedBlocksTotal  prometheus.Counter
	preparedTxsTotal     prometheus.Counter
	finishedBlocksTotal  prometheus.Counter
	finishedTxsTotal     prometheus.Counter
	successfulTxsTotal   prometheus.Counter
	failedTxsTotal       prometheus.Counter
	gasConsumedTotal     prometheus.Counter
	prepareErrorsTotal   prometheus.Counter
	executionErrorsTotal prometheus.Counter
	occAttemptsTotal     prometheus.Counter
	occFallbacksTotal    prometheus.Counter
	occRerunsTotal       prometheus.Counter
	occConflictsTotal    prometheus.Counter

	occFallbackReasonTotal *prometheus.CounterVec
	occConflictAccessTotal *prometheus.CounterVec
	sinkEnqueuedTotal      *prometheus.CounterVec
	sinkWrittenTotal       *prometheus.CounterVec
	sinkBytesTotal         *prometheus.CounterVec
	sinkEnqueueWaitTotal   prometheus.Counter
	sinkEnqueueWaitEvents  prometheus.Counter
	sinkWriteSecondsTotal  *prometheus.CounterVec

	inputBlockRate     prometheus.Gauge
	preparedBlockRate  prometheus.Gauge
	preparedTxRate     prometheus.Gauge
	finishedBlockRate  prometheus.Gauge
	txRate             prometheus.Gauge
	finishedTxRate     prometheus.Gauge
	failedTxRate       prometheus.Gauge
	gasRate            prometheus.Gauge
	queuedBlocks       prometheus.Gauge
	sinkQueuedRecords  prometheus.Gauge
	sinkQueueCapacity  prometheus.Gauge
	poolCapacityGauge  prometheus.Gauge
	poolAvailableGauge prometheus.Gauge
	poolOverflowGauge  prometheus.Gauge
}

type metricsSnapshot struct {
	at              time.Time
	inputBlocks     uint64
	preparedBlocks  uint64
	preparedTxs     uint64
	finishedBlocks  uint64
	finishedTxs     uint64
	successfulTxs   uint64
	failedTxs       uint64
	gasConsumed     uint64
	prepareErrors   uint64
	executionErrors uint64
	occAttempts     uint64
	occFallbacks    uint64
	occReruns       uint64
	occConflicts    uint64
	sinkEnqueued    uint64
	sinkWritten     uint64
	sinkBytes       uint64
	sinkWaitNanos   uint64
	sinkWaitEvents  uint64
	sinkWriteNanos  uint64
	sinkQueued      int64
	poolCapacity    int64
	poolAvailable   int64
	poolOverflow    uint64
}

func newLoadMetrics(registry *prometheus.Registry) *loadMetrics {
	m := &loadMetrics{
		inputBlocksTotal: prometheus.NewCounter(prometheus.CounterOpts{
			Name: "evmonly_loadtest_block_input_total",
			Help: "Total blocks fed to the EVM-only executor input queue.",
		}),
		preparedBlocksTotal: prometheus.NewCounter(prometheus.CounterOpts{
			Name: "evmonly_loadtest_block_prepared_total",
			Help: "Total blocks decoded and sender-recovered before EVM-only executor execution.",
		}),
		preparedTxsTotal: prometheus.NewCounter(prometheus.CounterOpts{
			Name: "evmonly_loadtest_transactions_prepared_total",
			Help: "Total transactions decoded and sender-recovered before EVM-only executor execution.",
		}),
		finishedBlocksTotal: prometheus.NewCounter(prometheus.CounterOpts{
			Name: "evmonly_loadtest_block_finished_total",
			Help: "Total blocks that finished EVM-only executor execution.",
		}),
		finishedTxsTotal: prometheus.NewCounter(prometheus.CounterOpts{
			Name: "evmonly_loadtest_transactions_finished_total",
			Help: "Total transactions that finished EVM-only executor execution.",
		}),
		successfulTxsTotal: prometheus.NewCounter(prometheus.CounterOpts{
			Name: "evmonly_loadtest_transactions_successful_total",
			Help: "Total successful transactions that finished EVM-only executor execution.",
		}),
		failedTxsTotal: prometheus.NewCounter(prometheus.CounterOpts{
			Name: "evmonly_loadtest_transactions_failed_total",
			Help: "Total failed transactions that finished EVM-only executor execution.",
		}),
		gasConsumedTotal: prometheus.NewCounter(prometheus.CounterOpts{
			Name: "evmonly_loadtest_gas_consumed_total",
			Help: "Total EVM gas consumed by finished blocks.",
		}),
		prepareErrorsTotal: prometheus.NewCounter(prometheus.CounterOpts{
			Name: "evmonly_loadtest_prepare_errors_total",
			Help: "Total block preparation errors returned while decoding transactions or recovering senders.",
		}),
		executionErrorsTotal: prometheus.NewCounter(prometheus.CounterOpts{
			Name: "evmonly_loadtest_execution_errors_total",
			Help: "Total block execution errors returned by the EVM-only executor.",
		}),
		occAttemptsTotal: prometheus.NewCounter(prometheus.CounterOpts{
			Name: "evmonly_loadtest_occ_attempts_total",
			Help: "Total blocks executed with optimistic concurrency control.",
		}),
		occFallbacksTotal: prometheus.NewCounter(prometheus.CounterOpts{
			Name: "evmonly_loadtest_occ_fallbacks_total",
			Help: "Total OCC blocks that fell back to sequential execution.",
		}),
		occRerunsTotal: prometheus.NewCounter(prometheus.CounterOpts{
			Name: "evmonly_loadtest_occ_reruns_total",
			Help: "Total OCC transaction rerun attempts caused by stale speculative execution state.",
		}),
		occConflictsTotal: prometheus.NewCounter(prometheus.CounterOpts{
			Name: "evmonly_loadtest_occ_conflicts_total",
			Help: "Total OCC conflict accesses observed before sequential fallback.",
		}),
		occFallbackReasonTotal: prometheus.NewCounterVec(prometheus.CounterOpts{
			Name: "evmonly_loadtest_occ_fallback_reasons_total",
			Help: "OCC fallback count by reason.",
		}, []string{"reason"}),
		occConflictAccessTotal: prometheus.NewCounterVec(prometheus.CounterOpts{
			Name: "evmonly_loadtest_occ_conflict_accesses_total",
			Help: "OCC conflict accesses by access type and state kind.",
		}, []string{"access", "kind"}),
		sinkEnqueuedTotal: prometheus.NewCounterVec(prometheus.CounterOpts{
			Name: "evmonly_loadtest_result_sink_records_enqueued_total",
			Help: "Total records accepted by the result sink.",
		}, []string{"kind"}),
		sinkWrittenTotal: prometheus.NewCounterVec(prometheus.CounterOpts{
			Name: "evmonly_loadtest_result_sink_records_written_total",
			Help: "Total records written by the result sink.",
		}, []string{"kind"}),
		sinkBytesTotal: prometheus.NewCounterVec(prometheus.CounterOpts{
			Name: "evmonly_loadtest_result_sink_bytes_written_total",
			Help: "Total bytes written by the result sink, including record framing.",
		}, []string{"kind"}),
		sinkEnqueueWaitTotal: prometheus.NewCounter(prometheus.CounterOpts{
			Name: "evmonly_loadtest_result_sink_enqueue_wait_seconds_total",
			Help: "Total time executor workers spent blocked enqueueing result sink records.",
		}),
		sinkEnqueueWaitEvents: prometheus.NewCounter(prometheus.CounterOpts{
			Name: "evmonly_loadtest_result_sink_enqueue_wait_events_total",
			Help: "Total result sink enqueue attempts that had to wait for queue capacity or cancellation.",
		}),
		sinkWriteSecondsTotal: prometheus.NewCounterVec(prometheus.CounterOpts{
			Name: "evmonly_loadtest_result_sink_write_seconds_total",
			Help: "Total time spent by result sink writers serializing, writing, flushing, and optionally syncing records.",
		}, []string{"kind"}),
		inputBlockRate: prometheus.NewGauge(prometheus.GaugeOpts{
			Name: "evmonly_loadtest_block_input_throughput",
			Help: "Most recent measured block input throughput in blocks per second.",
		}),
		preparedBlockRate: prometheus.NewGauge(prometheus.GaugeOpts{
			Name: "evmonly_loadtest_block_prepared_throughput",
			Help: "Most recent measured block preparation throughput in blocks per second.",
		}),
		preparedTxRate: prometheus.NewGauge(prometheus.GaugeOpts{
			Name: "evmonly_loadtest_transactions_prepared_per_second",
			Help: "Most recent measured transaction decode and sender recovery throughput in transactions per second.",
		}),
		finishedBlockRate: prometheus.NewGauge(prometheus.GaugeOpts{
			Name: "evmonly_loadtest_block_finished_throughput",
			Help: "Most recent measured block completion throughput in blocks per second.",
		}),
		txRate: prometheus.NewGauge(prometheus.GaugeOpts{
			Name: "evmonly_loadtest_transactions_per_second",
			Help: "Most recent measured successful transaction execution throughput in transactions per second.",
		}),
		finishedTxRate: prometheus.NewGauge(prometheus.GaugeOpts{
			Name: "evmonly_loadtest_transactions_finished_per_second",
			Help: "Most recent measured total finished transaction execution throughput in transactions per second.",
		}),
		failedTxRate: prometheus.NewGauge(prometheus.GaugeOpts{
			Name: "evmonly_loadtest_transactions_failed_per_second",
			Help: "Most recent measured failed transaction execution throughput in transactions per second.",
		}),
		gasRate: prometheus.NewGauge(prometheus.GaugeOpts{
			Name: "evmonly_loadtest_gas_consumed_per_second",
			Help: "Most recent measured gas consumption throughput in gas per second.",
		}),
		queuedBlocks: prometheus.NewGauge(prometheus.GaugeOpts{
			Name: "evmonly_loadtest_queued_blocks",
			Help: "Blocks currently waiting in the executor input queue.",
		}),
		sinkQueuedRecords: prometheus.NewGauge(prometheus.GaugeOpts{
			Name: "evmonly_loadtest_result_sink_queued_records",
			Help: "Persistent result sink records currently waiting for the async writer.",
		}),
		sinkQueueCapacity: prometheus.NewGauge(prometheus.GaugeOpts{
			Name: "evmonly_loadtest_result_sink_queue_capacity",
			Help: "Capacity of the persistent result sink async record queue.",
		}),
		poolCapacityGauge: prometheus.NewGauge(prometheus.GaugeOpts{
			Name: "evmonly_loadtest_result_pool_capacity",
			Help: "Capacity of the reusable executor block result pool.",
		}),
		poolAvailableGauge: prometheus.NewGauge(prometheus.GaugeOpts{
			Name: "evmonly_loadtest_result_pool_available",
			Help: "Reusable executor block result slots currently available.",
		}),
		poolOverflowGauge: prometheus.NewGauge(prometheus.GaugeOpts{
			Name: "evmonly_loadtest_result_pool_overflow_allocations",
			Help: "Total block results allocated outside the reusable pool because all slots were retained.",
		}),
	}
	registry.MustRegister(
		m.inputBlocksTotal,
		m.preparedBlocksTotal,
		m.preparedTxsTotal,
		m.finishedBlocksTotal,
		m.finishedTxsTotal,
		m.successfulTxsTotal,
		m.failedTxsTotal,
		m.gasConsumedTotal,
		m.prepareErrorsTotal,
		m.executionErrorsTotal,
		m.occAttemptsTotal,
		m.occFallbacksTotal,
		m.occRerunsTotal,
		m.occConflictsTotal,
		m.occFallbackReasonTotal,
		m.occConflictAccessTotal,
		m.sinkEnqueuedTotal,
		m.sinkWrittenTotal,
		m.sinkBytesTotal,
		m.sinkEnqueueWaitTotal,
		m.sinkEnqueueWaitEvents,
		m.sinkWriteSecondsTotal,
		m.inputBlockRate,
		m.preparedBlockRate,
		m.preparedTxRate,
		m.finishedBlockRate,
		m.txRate,
		m.finishedTxRate,
		m.failedTxRate,
		m.gasRate,
		m.queuedBlocks,
		m.sinkQueuedRecords,
		m.sinkQueueCapacity,
		m.poolCapacityGauge,
		m.poolAvailableGauge,
		m.poolOverflowGauge,
	)
	return m
}

func (m *loadMetrics) recordInput() {
	m.inputBlocks.Add(1)
	m.inputBlocksTotal.Inc()
}

func (m *loadMetrics) recordPrepared(txCount int) {
	m.preparedBlocks.Add(1)
	m.preparedTxs.Add(uint64FromNonNegativeInt(txCount))
	m.preparedBlocksTotal.Inc()
	m.preparedTxsTotal.Add(float64(txCount))
}

func (m *loadMetrics) recordPrepareError() {
	m.prepareErrors.Add(1)
	m.prepareErrorsTotal.Inc()
}

func (m *loadMetrics) recordFinished(counts txStatusCounts, gasUsed uint64, occStats evmonly.OCCStats) {
	m.finishedBlocks.Add(1)
	m.finishedTxs.Add(uint64FromNonNegativeInt(counts.total))
	m.successfulTxs.Add(uint64FromNonNegativeInt(counts.successful))
	m.failedTxs.Add(uint64FromNonNegativeInt(counts.failed))
	m.gasConsumed.Add(gasUsed)
	m.finishedBlocksTotal.Inc()
	m.finishedTxsTotal.Add(float64(counts.total))
	m.successfulTxsTotal.Add(float64(counts.successful))
	m.failedTxsTotal.Add(float64(counts.failed))
	m.gasConsumedTotal.Add(float64(gasUsed))
	m.recordOCC(occStats)
}

func (m *loadMetrics) recordOCC(stats evmonly.OCCStats) {
	if !stats.Attempted {
		return
	}
	m.occAttempts.Add(1)
	m.occAttemptsTotal.Inc()
	if stats.Fallback {
		reason := stats.FallbackReason
		if reason == "" {
			reason = "unknown"
		}
		m.occFallbacks.Add(1)
		m.occFallbacksTotal.Inc()
		m.occFallbackReasonTotal.WithLabelValues(reason).Inc()
	}
	if stats.RerunCount > 0 {
		m.occReruns.Add(stats.RerunCount)
		m.occRerunsTotal.Add(float64(stats.RerunCount))
	}
	if stats.ConflictCount == 0 {
		return
	}
	m.occConflicts.Add(stats.ConflictCount)
	m.occConflictsTotal.Add(float64(stats.ConflictCount))
	for _, conflict := range stats.ConflictSamples {
		m.occConflictAccessTotal.WithLabelValues(
			conflict.Access,
			conflict.Kind,
		).Add(float64(conflict.Count))
	}
}

func (m *loadMetrics) recordExecutionError() {
	m.executionErrors.Add(1)
	m.executionErrorsTotal.Inc()
}

func (m *loadMetrics) recordSinkEnqueued(kind string) {
	m.sinkEnqueued.Add(1)
	m.sinkEnqueuedTotal.WithLabelValues(kind).Inc()
}

func (m *loadMetrics) recordSinkEnqueueWait(elapsed time.Duration) {
	if elapsed <= 0 {
		return
	}
	m.sinkWaitNanos.Add(uint64FromNonNegativeInt64(elapsed.Nanoseconds()))
	m.sinkWaitEvents.Add(1)
	m.sinkEnqueueWaitTotal.Add(elapsed.Seconds())
	m.sinkEnqueueWaitEvents.Inc()
}

func (m *loadMetrics) recordSinkWrite(kind string, bytes int, elapsed time.Duration, completed bool) {
	if elapsed > 0 {
		m.sinkWriteNanos.Add(uint64FromNonNegativeInt64(elapsed.Nanoseconds()))
		m.sinkWriteSecondsTotal.WithLabelValues(kind).Add(elapsed.Seconds())
	}
	if !completed {
		return
	}
	m.sinkWritten.Add(1)
	m.sinkBytes.Add(uint64FromNonNegativeInt(bytes))
	m.sinkWrittenTotal.WithLabelValues(kind).Inc()
	m.sinkBytesTotal.WithLabelValues(kind).Add(float64(bytes))
}

func (m *loadMetrics) recordResultPoolStats(stats evmonly.BlockResultPoolStats) {
	m.poolCapacity.Store(int64(stats.Capacity))
	m.poolAvailable.Store(int64(stats.Available))
	m.poolOverflow.Store(stats.OverflowAllocations)
	m.poolCapacityGauge.Set(float64(stats.Capacity))
	m.poolAvailableGauge.Set(float64(stats.Available))
	m.poolOverflowGauge.Set(float64(stats.OverflowAllocations))
}

func (m *loadMetrics) setSinkQueued(records int) {
	m.sinkQueued.Store(int64(records))
	m.sinkQueuedRecords.Set(float64(records))
}

func (m *loadMetrics) setSinkQueueCapacity(records int) {
	m.sinkQueueCapacity.Set(float64(records))
}

func (m *loadMetrics) snapshot() metricsSnapshot {
	return metricsSnapshot{
		at:              time.Now(),
		inputBlocks:     m.inputBlocks.Load(),
		preparedBlocks:  m.preparedBlocks.Load(),
		preparedTxs:     m.preparedTxs.Load(),
		finishedBlocks:  m.finishedBlocks.Load(),
		finishedTxs:     m.finishedTxs.Load(),
		successfulTxs:   m.successfulTxs.Load(),
		failedTxs:       m.failedTxs.Load(),
		gasConsumed:     m.gasConsumed.Load(),
		prepareErrors:   m.prepareErrors.Load(),
		executionErrors: m.executionErrors.Load(),
		occAttempts:     m.occAttempts.Load(),
		occFallbacks:    m.occFallbacks.Load(),
		occReruns:       m.occReruns.Load(),
		occConflicts:    m.occConflicts.Load(),
		sinkEnqueued:    m.sinkEnqueued.Load(),
		sinkWritten:     m.sinkWritten.Load(),
		sinkBytes:       m.sinkBytes.Load(),
		sinkWaitNanos:   m.sinkWaitNanos.Load(),
		sinkWaitEvents:  m.sinkWaitEvents.Load(),
		sinkWriteNanos:  m.sinkWriteNanos.Load(),
		sinkQueued:      m.sinkQueued.Load(),
		poolCapacity:    m.poolCapacity.Load(),
		poolAvailable:   m.poolAvailable.Load(),
		poolOverflow:    m.poolOverflow.Load(),
	}
}

func (m *loadMetrics) setRates(prev, curr metricsSnapshot, queued int) {
	elapsed := curr.at.Sub(prev.at).Seconds()
	if elapsed <= 0 {
		return
	}
	inputRate := float64(curr.inputBlocks-prev.inputBlocks) / elapsed
	preparedRate := float64(curr.preparedBlocks-prev.preparedBlocks) / elapsed
	preparedTxRate := float64(curr.preparedTxs-prev.preparedTxs) / elapsed
	finishedRate := float64(curr.finishedBlocks-prev.finishedBlocks) / elapsed
	finishedTxRate := float64(curr.finishedTxs-prev.finishedTxs) / elapsed
	txRate := float64(curr.successfulTxs-prev.successfulTxs) / elapsed
	failedTxRate := float64(curr.failedTxs-prev.failedTxs) / elapsed
	gasRate := float64(curr.gasConsumed-prev.gasConsumed) / elapsed
	m.inputBlockRate.Set(inputRate)
	m.preparedBlockRate.Set(preparedRate)
	m.preparedTxRate.Set(preparedTxRate)
	m.finishedBlockRate.Set(finishedRate)
	m.txRate.Set(txRate)
	m.finishedTxRate.Set(finishedTxRate)
	m.failedTxRate.Set(failedTxRate)
	m.gasRate.Set(gasRate)
	m.queuedBlocks.Set(float64(queued))
}

func reportLoop(ctx context.Context, interval time.Duration, metrics *loadMetrics, blocks <-chan preparedBlockEnvelope) {
	if interval == 0 {
		<-ctx.Done()
		return
	}
	ticker := time.NewTicker(interval)
	defer ticker.Stop()

	prev := metrics.snapshot()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			curr := metrics.snapshot()
			queued := len(blocks)
			metrics.setRates(prev, curr, queued)
			elapsed := curr.at.Sub(prev.at).Seconds()
			if elapsed <= 0 {
				prev = curr
				continue
			}
			sinkWaitSeconds := float64(curr.sinkWaitNanos-prev.sinkWaitNanos) / float64(time.Second)
			sinkWriteSeconds := float64(curr.sinkWriteNanos-prev.sinkWriteNanos) / float64(time.Second)
			fmt.Printf(
				"input_blocks/s=%.2f prepared_blocks/s=%.2f prepared_tx/s=%.2f finished_blocks/s=%.2f finished_tx/s=%.2f success_tx/s=%.2f failed_tx/s=%.2f gas/s=%.2f queued_blocks=%d sink_queue=%d pool_available=%d pool_overflow=%d sink_enqueue_wait/s=%.6f sink_write/s=%.6f totals(input_blocks=%d prepared_blocks=%d prepared_txs=%d finished_blocks=%d txs=%d successful_txs=%d failed_txs=%d gas=%d prepare_errors=%d errors=%d occ_attempts=%d occ_fallbacks=%d occ_reruns=%d sink_enqueued=%d sink_written=%d)\n",
				float64(curr.inputBlocks-prev.inputBlocks)/elapsed,
				float64(curr.preparedBlocks-prev.preparedBlocks)/elapsed,
				float64(curr.preparedTxs-prev.preparedTxs)/elapsed,
				float64(curr.finishedBlocks-prev.finishedBlocks)/elapsed,
				float64(curr.finishedTxs-prev.finishedTxs)/elapsed,
				float64(curr.successfulTxs-prev.successfulTxs)/elapsed,
				float64(curr.failedTxs-prev.failedTxs)/elapsed,
				float64(curr.gasConsumed-prev.gasConsumed)/elapsed,
				queued,
				curr.sinkQueued,
				curr.poolAvailable,
				curr.poolOverflow,
				sinkWaitSeconds/elapsed,
				sinkWriteSeconds/elapsed,
				curr.inputBlocks,
				curr.preparedBlocks,
				curr.preparedTxs,
				curr.finishedBlocks,
				curr.finishedTxs,
				curr.successfulTxs,
				curr.failedTxs,
				curr.gasConsumed,
				curr.prepareErrors,
				curr.executionErrors,
				curr.occAttempts,
				curr.occFallbacks,
				curr.occReruns,
				curr.sinkEnqueued,
				curr.sinkWritten,
			)
			prev = curr
		}
	}
}

func printFinalReport(startedAt time.Time, snapshot metricsSnapshot) {
	elapsed := snapshot.at.Sub(startedAt).Seconds()
	if elapsed <= 0 {
		elapsed = 1
	}
	fmt.Printf(
		"complete elapsed=%s input_blocks=%d prepared_blocks=%d prepared_txs=%d finished_blocks=%d txs=%d successful_txs=%d failed_txs=%d gas=%d prepare_errors=%d errors=%d occ_attempts=%d occ_fallbacks=%d occ_reruns=%d sink_queue=%d sink_enqueued=%d sink_written=%d sink_bytes=%d sink_enqueue_wait=%s sink_enqueue_wait_events=%d sink_write=%s pool_capacity=%d pool_available=%d pool_overflow=%d avg_input_blocks/s=%.2f avg_prepared_blocks/s=%.2f avg_prepared_tx/s=%.2f avg_finished_blocks/s=%.2f avg_finished_tx/s=%.2f avg_success_tx/s=%.2f avg_failed_tx/s=%.2f avg_gas/s=%.2f\n",
		snapshot.at.Sub(startedAt).Round(time.Millisecond),
		snapshot.inputBlocks,
		snapshot.preparedBlocks,
		snapshot.preparedTxs,
		snapshot.finishedBlocks,
		snapshot.finishedTxs,
		snapshot.successfulTxs,
		snapshot.failedTxs,
		snapshot.gasConsumed,
		snapshot.prepareErrors,
		snapshot.executionErrors,
		snapshot.occAttempts,
		snapshot.occFallbacks,
		snapshot.occReruns,
		snapshot.sinkQueued,
		snapshot.sinkEnqueued,
		snapshot.sinkWritten,
		snapshot.sinkBytes,
		durationFromUint64Nanos(snapshot.sinkWaitNanos).Round(time.Microsecond),
		snapshot.sinkWaitEvents,
		durationFromUint64Nanos(snapshot.sinkWriteNanos).Round(time.Microsecond),
		snapshot.poolCapacity,
		snapshot.poolAvailable,
		snapshot.poolOverflow,
		float64(snapshot.inputBlocks)/elapsed,
		float64(snapshot.preparedBlocks)/elapsed,
		float64(snapshot.preparedTxs)/elapsed,
		float64(snapshot.finishedBlocks)/elapsed,
		float64(snapshot.finishedTxs)/elapsed,
		float64(snapshot.successfulTxs)/elapsed,
		float64(snapshot.failedTxs)/elapsed,
		float64(snapshot.gasConsumed)/elapsed,
	)
}

func printResultSinkReport(closeElapsed time.Duration, snapshot metricsSnapshot) {
	fmt.Printf(
		"result sink close elapsed=%s sink_queue=%d sink_enqueued=%d sink_written=%d sink_bytes=%d sink_enqueue_wait=%s sink_enqueue_wait_events=%d sink_write=%s\n",
		closeElapsed.Round(time.Millisecond),
		snapshot.sinkQueued,
		snapshot.sinkEnqueued,
		snapshot.sinkWritten,
		snapshot.sinkBytes,
		durationFromUint64Nanos(snapshot.sinkWaitNanos).Round(time.Microsecond),
		snapshot.sinkWaitEvents,
		durationFromUint64Nanos(snapshot.sinkWriteNanos).Round(time.Microsecond),
	)
}

func printPrebuildReport(elapsed time.Duration, blocks []blockEnvelope, txsPerBlock int) {
	seconds := elapsed.Seconds()
	if seconds <= 0 {
		seconds = 1
	}
	txCount := len(blocks) * txsPerBlock
	fmt.Printf(
		"prebuild complete elapsed=%s blocks=%d txs=%d build_blocks/s=%.2f build_tx/s=%.2f\n",
		elapsed.Round(time.Millisecond),
		len(blocks),
		txCount,
		float64(len(blocks))/seconds,
		float64(txCount)/seconds,
	)
}

type metricsServer struct {
	server *http.Server
	done   chan error
}

func startMetricsServer(addr string, registry *prometheus.Registry) (*metricsServer, error) {
	listener, err := net.Listen("tcp", addr)
	if err != nil {
		return nil, fmt.Errorf("listen for metrics on %s: %w", addr, err)
	}
	mux := http.NewServeMux()
	mux.Handle("/metrics", promhttp.HandlerFor(registry, promhttp.HandlerOpts{}))
	mux.HandleFunc("/healthz", func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte("ok\n"))
	})
	server := &http.Server{
		Addr:              addr,
		Handler:           mux,
		ReadHeaderTimeout: 3 * time.Second,
	}
	ms := &metricsServer{server: server, done: make(chan error, 1)}
	go func() {
		err := server.Serve(listener)
		if errors.Is(err, http.ErrServerClosed) {
			err = nil
		}
		ms.done <- err
	}()
	return ms, nil
}

func (s *metricsServer) stop(timeout time.Duration) error {
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()
	if err := s.server.Shutdown(ctx); err != nil {
		return err
	}
	return <-s.done
}
