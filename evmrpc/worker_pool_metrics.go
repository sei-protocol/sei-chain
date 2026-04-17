package evmrpc

import (
	"context"
	"fmt"
	"os"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/sei-protocol/sei-chain/sei-cosmos/telemetry"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/metric"
)

// Environment variable to enable debug metrics printing to stdout
// Set EVM_DEBUG_METRICS=true to enable periodic metrics printing
const EVMDebugMetricsEnvVar = "EVM_DEBUG_METRICS"

// IsDebugMetricsEnabled checks if debug metrics printing is enabled via environment variable
func IsDebugMetricsEnabled() bool {
	val := os.Getenv(EVMDebugMetricsEnvVar)
	return strings.ToLower(val) == "true" || val == "1"
}

// Error type constants for categorization
const (
	ErrTypeRangeTooLarge = "range_too_large"
	ErrTypeTimeout       = "timeout"
	ErrTypeRateLimited   = "rate_limited"
	ErrTypeBackpressure  = "backpressure"
	ErrTypeIOSaturated   = "io_saturated"
	ErrTypeBlockNotFound = "block_not_found"
	ErrTypeQueueFull     = "queue_full"
	ErrTypeOther         = "other"
)

// WorkerPoolMetrics tracks worker pool performance metrics
type WorkerPoolMetrics struct {
	// Worker pool stats
	TotalWorkers    atomic.Int32
	ActiveWorkers   atomic.Int32
	QueueCapacity   atomic.Int32
	QueueDepth      atomic.Int32
	PeakQueueDepth  atomic.Int32
	TasksSubmitted  atomic.Int64
	TasksCompleted  atomic.Int64
	TasksRejected   atomic.Int64 // Queue full rejections
	TasksPanicked   atomic.Int64
	TotalWaitTimeNs atomic.Int64 // Total time tasks spent waiting in queue
	TotalExecTimeNs atomic.Int64 // Total task execution time

	// DB Semaphore stats
	DBSemaphoreCapacity   atomic.Int32
	DBSemaphoreAcquired   atomic.Int32
	DBSemaphoreWaitTimeNs atomic.Int64
	DBSemaphoreWaitCount  atomic.Int64

	// eth_getLogs specific stats
	GetLogsRequests      atomic.Int64
	GetLogsErrors        atomic.Int64
	GetLogsSuccess       atomic.Int64 // Successful requests
	GetLogsBlockRangeSum atomic.Int64 // Sum of block ranges for average calculation
	GetLogsLatencySumNs  atomic.Int64 // Sum of latencies for average calculation
	GetLogsPeakRange     atomic.Int64
	GetLogsMaxLatencyNs  atomic.Int64 // Max latency observed

	// Error type breakdown
	ErrRangeTooLarge atomic.Int64
	ErrTimeout       atomic.Int64
	ErrRateLimited   atomic.Int64
	ErrBackpressure  atomic.Int64
	ErrIOSaturated   atomic.Int64
	ErrBlockNotFound atomic.Int64
	ErrQueueFull     atomic.Int64
	ErrOther         atomic.Int64

	// Block range distribution buckets (total requests)
	RangeBucket1to10      atomic.Int64 // 1-10 blocks
	RangeBucket11to100    atomic.Int64 // 11-100 blocks
	RangeBucket101to500   atomic.Int64 // 101-500 blocks
	RangeBucket501to1000  atomic.Int64 // 501-1000 blocks
	RangeBucket1001to2000 atomic.Int64 // 1001-2000 blocks
	RangeBucketOver2000   atomic.Int64 // >2000 blocks

	// Block range success counts (for calculating success rate per bucket)
	RangeBucket1to10Success      atomic.Int64
	RangeBucket11to100Success    atomic.Int64
	RangeBucket101to500Success   atomic.Int64
	RangeBucket501to1000Success  atomic.Int64
	RangeBucket1001to2000Success atomic.Int64
	RangeBucketOver2000Success   atomic.Int64

	// Subscription stats
	ActiveSubscriptions atomic.Int32
	SubscriptionErrors  atomic.Int64

	// Time window for TPS calculation
	windowStart    time.Time
	windowRequests atomic.Int64
	mu             sync.RWMutex
}

var (
	metricsPrinterOnce sync.Once
	metricsStopOnce    sync.Once
	metricsStopChan    chan struct{}
)

var (
	meter = otel.Meter("evmrpc_workerpool")

	otelMetrics = struct {
		workersTotal          metric.Int64Gauge
		workersActive         metric.Int64Gauge
		workersIdle           metric.Int64Gauge
		queueCapacity         metric.Int64Gauge
		queueDepth            metric.Int64Gauge
		queuePeak             metric.Int64Gauge
		queueUtilizationPct   metric.Float64Gauge
		tasksSubmittedTotal   metric.Int64Gauge
		tasksCompletedTotal   metric.Int64Gauge
		tasksRejectedTotal    metric.Int64Gauge
		tasksPanickedTotal    metric.Int64Gauge
		dbSemaphoreCapacity   metric.Int64Gauge
		dbSemaphoreInUse      metric.Int64Gauge
		dbSemaphoreAvailable  metric.Int64Gauge
		dbSemaphoreWaitCount  metric.Int64Gauge
		subscriptionsActive   metric.Int64Gauge
		getLogsRequestsTotal  metric.Int64Gauge
		getLogsSuccessTotal   metric.Int64Gauge
		getLogsErrorsTotal    metric.Int64Gauge
		getLogsTPS            metric.Float64Gauge
		getLogsAvgBlockRange  metric.Float64Gauge
		getLogsPeakBlockRange metric.Int64Gauge
		getLogsAvgLatencyMs   metric.Float64Gauge
		getLogsMaxLatencyMs   metric.Float64Gauge
		errRangeTooLarge      metric.Int64Gauge
		errRateLimited        metric.Int64Gauge
		errBackpressure       metric.Int64Gauge
		avgQueueWaitMs        metric.Float64Gauge
		avgExecTimeMs         metric.Float64Gauge
		avgDBWaitMs           metric.Float64Gauge
	}{
		workersTotal: must(meter.Int64Gauge(
			"evmrpc_workerpool_workers_total",
			metric.WithDescription("Total worker count"),
			metric.WithUnit("{count}"),
		)),
		workersActive: must(meter.Int64Gauge(
			"evmrpc_workerpool_workers_active",
			metric.WithDescription("Active worker count"),
			metric.WithUnit("{count}"),
		)),
		workersIdle: must(meter.Int64Gauge(
			"evmrpc_workerpool_workers_idle",
			metric.WithDescription("Idle worker count"),
			metric.WithUnit("{count}"),
		)),
		queueCapacity: must(meter.Int64Gauge(
			"evmrpc_workerpool_queue_capacity",
			metric.WithDescription("Task queue capacity"),
			metric.WithUnit("{count}"),
		)),
		queueDepth: must(meter.Int64Gauge(
			"evmrpc_workerpool_queue_depth",
			metric.WithDescription("Current task queue depth"),
			metric.WithUnit("{count}"),
		)),
		queuePeak: must(meter.Int64Gauge(
			"evmrpc_workerpool_queue_peak",
			metric.WithDescription("Peak queue depth observed"),
			metric.WithUnit("{count}"),
		)),
		queueUtilizationPct: must(meter.Float64Gauge(
			"evmrpc_workerpool_queue_utilization_pct",
			metric.WithDescription("Queue utilization percentage"),
			metric.WithUnit("1"),
		)),
		tasksSubmittedTotal: must(meter.Int64Gauge(
			"evmrpc_workerpool_tasks_submitted_total",
			metric.WithDescription("Tasks submitted"),
			metric.WithUnit("{count}"),
		)),
		tasksCompletedTotal: must(meter.Int64Gauge(
			"evmrpc_workerpool_tasks_completed_total",
			metric.WithDescription("Tasks completed"),
			metric.WithUnit("{count}"),
		)),
		tasksRejectedTotal: must(meter.Int64Gauge(
			"evmrpc_workerpool_tasks_rejected_total",
			metric.WithDescription("Tasks rejected due to full queue"),
			metric.WithUnit("{count}"),
		)),
		tasksPanickedTotal: must(meter.Int64Gauge(
			"evmrpc_workerpool_tasks_panicked_total",
			metric.WithDescription("Tasks that panicked"),
			metric.WithUnit("{count}"),
		)),
		dbSemaphoreCapacity: must(meter.Int64Gauge(
			"evmrpc_db_semaphore_capacity",
			metric.WithDescription("DB semaphore capacity"),
			metric.WithUnit("{count}"),
		)),
		dbSemaphoreInUse: must(meter.Int64Gauge(
			"evmrpc_db_semaphore_inuse",
			metric.WithDescription("DB semaphore currently acquired"),
			metric.WithUnit("{count}"),
		)),
		dbSemaphoreAvailable: must(meter.Int64Gauge(
			"evmrpc_db_semaphore_available",
			metric.WithDescription("DB semaphore available slots"),
			metric.WithUnit("{count}"),
		)),
		dbSemaphoreWaitCount: must(meter.Int64Gauge(
			"evmrpc_db_semaphore_wait_count",
			metric.WithDescription("DB semaphore wait count"),
			metric.WithUnit("{count}"),
		)),
		subscriptionsActive: must(meter.Int64Gauge(
			"evmrpc_subscriptions_active",
			metric.WithDescription("Active subscriptions"),
			metric.WithUnit("{count}"),
		)),
		getLogsRequestsTotal: must(meter.Int64Gauge(
			"evmrpc_getlogs_requests_total",
			metric.WithDescription("Total eth_getLogs requests"),
			metric.WithUnit("{count}"),
		)),
		getLogsSuccessTotal: must(meter.Int64Gauge(
			"evmrpc_getlogs_success_total",
			metric.WithDescription("Successful eth_getLogs requests"),
			metric.WithUnit("{count}"),
		)),
		getLogsErrorsTotal: must(meter.Int64Gauge(
			"evmrpc_getlogs_errors_total",
			metric.WithDescription("Errored eth_getLogs requests"),
			metric.WithUnit("{count}"),
		)),
		getLogsTPS: must(meter.Float64Gauge(
			"evmrpc_getlogs_tps",
			metric.WithDescription("eth_getLogs throughput (req/s)"),
			metric.WithUnit("1/s"),
		)),
		getLogsAvgBlockRange: must(meter.Float64Gauge(
			"evmrpc_getlogs_avg_blockrange",
			metric.WithDescription("Average block range for eth_getLogs"),
			metric.WithUnit("{blocks}"),
		)),
		getLogsPeakBlockRange: must(meter.Int64Gauge(
			"evmrpc_getlogs_peak_blockrange",
			metric.WithDescription("Peak block range for eth_getLogs"),
			metric.WithUnit("{blocks}"),
		)),
		getLogsAvgLatencyMs: must(meter.Float64Gauge(
			"evmrpc_getlogs_avg_latency_ms",
			metric.WithDescription("Average eth_getLogs latency (ms)"),
			metric.WithUnit("ms"),
		)),
		getLogsMaxLatencyMs: must(meter.Float64Gauge(
			"evmrpc_getlogs_max_latency_ms",
			metric.WithDescription("Max eth_getLogs latency (ms)"),
			metric.WithUnit("ms"),
		)),
		errRangeTooLarge: must(meter.Int64Gauge(
			"evmrpc_getlogs_errors_range_too_large",
			metric.WithDescription("Errors due to block range too large"),
			metric.WithUnit("{count}"),
		)),
		errRateLimited: must(meter.Int64Gauge(
			"evmrpc_getlogs_errors_rate_limited",
			metric.WithDescription("Errors due to rate limiting"),
			metric.WithUnit("{count}"),
		)),
		errBackpressure: must(meter.Int64Gauge(
			"evmrpc_getlogs_errors_backpressure",
			metric.WithDescription("Errors due to backpressure"),
			metric.WithUnit("{count}"),
		)),
		avgQueueWaitMs: must(meter.Float64Gauge(
			"evmrpc_workerpool_avg_queue_wait_ms",
			metric.WithDescription("Average queue wait time (ms)"),
			metric.WithUnit("ms"),
		)),
		avgExecTimeMs: must(meter.Float64Gauge(
			"evmrpc_workerpool_avg_exec_time_ms",
			metric.WithDescription("Average execution time (ms)"),
			metric.WithUnit("ms"),
		)),
		avgDBWaitMs: must(meter.Float64Gauge(
			"evmrpc_db_semaphore_avg_wait_ms",
			metric.WithDescription("Average DB semaphore wait (ms)"),
			metric.WithUnit("ms"),
		)),
	}
)

// GetGlobalMetrics returns the metrics from the global worker pool
// This is a convenience function for accessing metrics without importing worker pool
func GetGlobalMetrics() *WorkerPoolMetrics {
	return GetGlobalWorkerPool().Metrics
}

// StartMetricsPrinter starts a background goroutine that prints metrics every interval
// This is idempotent - only the first call will start the printer
// Note: Printing to stdout is controlled by the EVM_DEBUG_METRICS environment variable
// Set EVM_DEBUG_METRICS=true to enable debug output
func StartMetricsPrinter(interval time.Duration) {
	metricsPrinterOnce.Do(func() {
		metricsStopChan = make(chan struct{})
		debugEnabled := IsDebugMetricsEnabled()
		go func() {
			ticker := time.NewTicker(interval)
			defer func() {
				ticker.Stop()
				metricsStopChan = nil
			}()

			for {
				select {
				case <-ticker.C:
					m := GetGlobalMetrics()
					// Export to Prometheus (gauges need periodic update)
					m.ExportPrometheusMetrics()
					// Print to stdout only if debug is enabled
					if debugEnabled {
						m.PrintMetrics()
					}
				case <-metricsStopChan:
					return
				}
			}

		}()
	})
}

// StopMetricsPrinter stops the metrics printer, idempotent.
func StopMetricsPrinter() {
	metricsStopOnce.Do(func() {
		if metricsStopChan != nil {
			close(metricsStopChan)
		}
	})
}

// RecordTaskSubmitted records a task submission
// Note: Prometheus export is done in batch via ExportPrometheusMetrics()
func (m *WorkerPoolMetrics) RecordTaskSubmitted() {
	m.TasksSubmitted.Add(1)
	depth := m.QueueDepth.Add(1)
	// Update peak if needed
	for {
		peak := m.PeakQueueDepth.Load()
		if depth <= peak || m.PeakQueueDepth.CompareAndSwap(peak, depth) {
			break
		}
	}
}

// RecordTaskStarted records when a task starts executing
// Note: Prometheus export is done in batch via ExportPrometheusMetrics()
func (m *WorkerPoolMetrics) RecordTaskStarted(queuedAt time.Time) {
	m.ActiveWorkers.Add(1)
	m.QueueDepth.Add(-1)
	waitTime := time.Since(queuedAt)
	m.TotalWaitTimeNs.Add(waitTime.Nanoseconds())
}

// RecordTaskCompleted records a task completion
// Note: Prometheus export is done in batch via ExportPrometheusMetrics()
func (m *WorkerPoolMetrics) RecordTaskCompleted(startedAt time.Time) {
	m.ActiveWorkers.Add(-1)
	m.TasksCompleted.Add(1)
	execTime := time.Since(startedAt)
	m.TotalExecTimeNs.Add(execTime.Nanoseconds())
}

// RecordTaskRejected records a task rejection (queue full)
// Note: Prometheus export is done in batch via ExportPrometheusMetrics()
func (m *WorkerPoolMetrics) RecordTaskRejected() {
	m.TasksRejected.Add(1)
}

// RecordTaskPanicked records a task panic
// Note: Prometheus export is done in batch via ExportPrometheusMetrics()
func (m *WorkerPoolMetrics) RecordTaskPanicked() {
	m.TasksPanicked.Add(1)
}

// RecordDBSemaphoreAcquire records acquiring the DB semaphore
func (m *WorkerPoolMetrics) RecordDBSemaphoreAcquire() {
	m.DBSemaphoreAcquired.Add(1)
}

// RecordDBSemaphoreRelease records releasing the DB semaphore
func (m *WorkerPoolMetrics) RecordDBSemaphoreRelease() {
	m.DBSemaphoreAcquired.Add(-1)
}

// RecordDBSemaphoreWait records time spent waiting for DB semaphore
// Note: Prometheus export is done in batch via ExportPrometheusMetrics()
func (m *WorkerPoolMetrics) RecordDBSemaphoreWait(waitTime time.Duration) {
	m.DBSemaphoreWaitTimeNs.Add(waitTime.Nanoseconds())
	m.DBSemaphoreWaitCount.Add(1)
}

// RecordGetLogsRequest records an eth_getLogs request with detailed error categorization
func (m *WorkerPoolMetrics) RecordGetLogsRequest(blockRange int64, latency time.Duration, startTime time.Time, err error) {
	m.GetLogsRequests.Add(1)
	m.windowRequests.Add(1)
	m.GetLogsBlockRangeSum.Add(blockRange)
	m.GetLogsLatencySumNs.Add(latency.Nanoseconds())

	// Update max latency
	latencyNs := latency.Nanoseconds()
	for {
		maxLat := m.GetLogsMaxLatencyNs.Load()
		if latencyNs <= maxLat || m.GetLogsMaxLatencyNs.CompareAndSwap(maxLat, latencyNs) {
			break
		}
	}

	// Update peak range
	for {
		peak := m.GetLogsPeakRange.Load()
		if blockRange <= peak || m.GetLogsPeakRange.CompareAndSwap(peak, blockRange) {
			break
		}
	}

	// Record block range distribution
	m.recordBlockRangeBucket(blockRange)

	// Categorize errors and record success per bucket
	if err != nil {
		m.GetLogsErrors.Add(1)
		m.categorizeError(err)
	} else {
		m.GetLogsSuccess.Add(1)
		m.recordBlockRangeBucketSuccess(blockRange)
	}
	// Note: Prometheus export is done in batch via ExportPrometheusMetrics()
}

// recordBlockRangeBucket records the block range into the appropriate bucket
func (m *WorkerPoolMetrics) recordBlockRangeBucket(blockRange int64) {
	switch {
	case blockRange <= 10:
		m.RangeBucket1to10.Add(1)
	case blockRange <= 100:
		m.RangeBucket11to100.Add(1)
	case blockRange <= 500:
		m.RangeBucket101to500.Add(1)
	case blockRange <= 1000:
		m.RangeBucket501to1000.Add(1)
	case blockRange <= 2000:
		m.RangeBucket1001to2000.Add(1)
	default:
		m.RangeBucketOver2000.Add(1)
	}
}

// recordBlockRangeBucketSuccess records a successful request in the appropriate bucket
func (m *WorkerPoolMetrics) recordBlockRangeBucketSuccess(blockRange int64) {
	switch {
	case blockRange <= 10:
		m.RangeBucket1to10Success.Add(1)
	case blockRange <= 100:
		m.RangeBucket11to100Success.Add(1)
	case blockRange <= 500:
		m.RangeBucket101to500Success.Add(1)
	case blockRange <= 1000:
		m.RangeBucket501to1000Success.Add(1)
	case blockRange <= 2000:
		m.RangeBucket1001to2000Success.Add(1)
	default:
		m.RangeBucketOver2000Success.Add(1)
	}
}

// categorizeError categorizes an error into specific types
func (m *WorkerPoolMetrics) categorizeError(err error) {
	if err == nil {
		return
	}
	errStr := err.Error()

	switch {
	case contains(errStr, "block range too large"):
		m.ErrRangeTooLarge.Add(1)
	case contains(errStr, "timeout") || contains(errStr, "deadline exceeded") || contains(errStr, "request timed out"):
		m.ErrTimeout.Add(1)
	case contains(errStr, "rate limit"):
		m.ErrRateLimited.Add(1)
	case contains(errStr, "server too busy") || contains(errStr, "pending"):
		m.ErrBackpressure.Add(1)
	case contains(errStr, "I/O saturated") || contains(errStr, "semaphore"):
		m.ErrIOSaturated.Add(1)
	case contains(errStr, "block not found") || contains(errStr, "height is not available") || contains(errStr, "pruned blocks"):
		m.ErrBlockNotFound.Add(1)
	case contains(errStr, "queue is full") || contains(errStr, "system overloaded"):
		m.ErrQueueFull.Add(1)
	default:
		m.ErrOther.Add(1)
	}
}

// contains is a helper function for case-insensitive substring matching
func contains(s, substr string) bool {
	return len(s) >= len(substr) && (s == substr ||
		len(substr) == 0 ||
		(len(s) > 0 && len(substr) > 0 && containsLower(toLower(s), toLower(substr))))
}

func toLower(s string) string {
	b := make([]byte, len(s))
	for i := 0; i < len(s); i++ {
		c := s[i]
		if c >= 'A' && c <= 'Z' {
			c += 'a' - 'A'
		}
		b[i] = c
	}
	return string(b)
}

func containsLower(s, substr string) bool {
	for i := 0; i <= len(s)-len(substr); i++ {
		if s[i:i+len(substr)] == substr {
			return true
		}
	}
	return false
}

// RecordSubscriptionStart records a new subscription
func (m *WorkerPoolMetrics) RecordSubscriptionStart() {
	m.ActiveSubscriptions.Add(1)
}

// RecordSubscriptionEnd records subscription end
func (m *WorkerPoolMetrics) RecordSubscriptionEnd() {
	m.ActiveSubscriptions.Add(-1)
}

// RecordSubscriptionError records a subscription error
func (m *WorkerPoolMetrics) RecordSubscriptionError() {
	m.SubscriptionErrors.Add(1)
	// Export to Prometheus
	telemetry.IncrCounter(1, "sei", "evm", "subscriptions", "errors")
}

// GetTPS calculates the current TPS based on time window
func (m *WorkerPoolMetrics) GetTPS() float64 {
	m.mu.RLock()
	windowStart := m.windowStart
	requests := m.windowRequests.Load()
	m.mu.RUnlock()

	elapsed := time.Since(windowStart).Seconds()
	if elapsed <= 0 {
		return 0
	}
	return float64(requests) / elapsed
}

// ResetTPSWindow resets the TPS calculation window
func (m *WorkerPoolMetrics) ResetTPSWindow() {
	m.mu.Lock()
	m.windowStart = time.Now()
	m.windowRequests.Store(0)
	m.mu.Unlock()
}

// GetAverageQueueWaitTime returns average time tasks spend waiting in queue
func (m *WorkerPoolMetrics) GetAverageQueueWaitTime() time.Duration {
	completed := m.TasksCompleted.Load()
	if completed == 0 {
		return 0
	}
	return time.Duration(m.TotalWaitTimeNs.Load() / completed)
}

// GetAverageExecTime returns average task execution time
func (m *WorkerPoolMetrics) GetAverageExecTime() time.Duration {
	completed := m.TasksCompleted.Load()
	if completed == 0 {
		return 0
	}
	return time.Duration(m.TotalExecTimeNs.Load() / completed)
}

// GetAverageDBWaitTime returns average DB semaphore wait time
func (m *WorkerPoolMetrics) GetAverageDBWaitTime() time.Duration {
	count := m.DBSemaphoreWaitCount.Load()
	if count == 0 {
		return 0
	}
	return time.Duration(m.DBSemaphoreWaitTimeNs.Load() / count)
}

// GetAverageBlockRange returns average block range for eth_getLogs
func (m *WorkerPoolMetrics) GetAverageBlockRange() float64 {
	requests := m.GetLogsRequests.Load()
	if requests == 0 {
		return 0
	}
	return float64(m.GetLogsBlockRangeSum.Load()) / float64(requests)
}

// GetAverageLatency returns average eth_getLogs latency
func (m *WorkerPoolMetrics) GetAverageLatency() time.Duration {
	requests := m.GetLogsRequests.Load()
	if requests == 0 {
		return 0
	}
	return time.Duration(m.GetLogsLatencySumNs.Load() / requests)
}

// GetSnapshot returns a snapshot of current metrics
func (m *WorkerPoolMetrics) GetSnapshot() MetricsSnapshot {
	totalWorkers := m.TotalWorkers.Load()
	queueCap := m.QueueCapacity.Load()
	dbSemCap := m.DBSemaphoreCapacity.Load()

	return MetricsSnapshot{
		Timestamp: time.Now(),

		// Worker pool
		TotalWorkers:     totalWorkers,
		ActiveWorkers:    m.ActiveWorkers.Load(),
		IdleWorkers:      totalWorkers - m.ActiveWorkers.Load(),
		QueueCapacity:    queueCap,
		QueueDepth:       m.QueueDepth.Load(),
		QueueUtilization: float64(m.QueueDepth.Load()) / float64(max(queueCap, 1)) * 100,
		PeakQueueDepth:   m.PeakQueueDepth.Load(),
		TasksSubmitted:   m.TasksSubmitted.Load(),
		TasksCompleted:   m.TasksCompleted.Load(),
		TasksRejected:    m.TasksRejected.Load(),
		TasksPending:     m.TasksSubmitted.Load() - m.TasksCompleted.Load() - m.TasksRejected.Load(),
		AvgQueueWaitTime: m.GetAverageQueueWaitTime(),
		AvgExecTime:      m.GetAverageExecTime(),

		// DB Semaphore
		DBSemaphoreCapacity: dbSemCap,
		DBSemaphoreInUse:    m.DBSemaphoreAcquired.Load(),
		DBSemaphoreAvail:    dbSemCap - m.DBSemaphoreAcquired.Load(),
		AvgDBWaitTime:       m.GetAverageDBWaitTime(),

		// eth_getLogs
		GetLogsTPS:       m.GetTPS(),
		GetLogsTotal:     m.GetLogsRequests.Load(),
		GetLogsSuccess:   m.GetLogsSuccess.Load(),
		GetLogsErrors:    m.GetLogsErrors.Load(),
		GetLogsErrorRate: float64(m.GetLogsErrors.Load()) / float64(max(m.GetLogsRequests.Load(), 1)) * 100,
		AvgBlockRange:    m.GetAverageBlockRange(),
		PeakBlockRange:   m.GetLogsPeakRange.Load(),
		AvgLatency:       m.GetAverageLatency(),
		MaxLatency:       time.Duration(m.GetLogsMaxLatencyNs.Load()),

		// Error type breakdown
		ErrRangeTooLarge: m.ErrRangeTooLarge.Load(),
		ErrTimeout:       m.ErrTimeout.Load(),
		ErrRateLimited:   m.ErrRateLimited.Load(),
		ErrBackpressure:  m.ErrBackpressure.Load(),
		ErrIOSaturated:   m.ErrIOSaturated.Load(),
		ErrBlockNotFound: m.ErrBlockNotFound.Load(),
		ErrQueueFull:     m.ErrQueueFull.Load(),
		ErrOther:         m.ErrOther.Load(),

		// Block range distribution
		RangeBucket1to10:             m.RangeBucket1to10.Load(),
		RangeBucket1to10Success:      m.RangeBucket1to10Success.Load(),
		RangeBucket11to100:           m.RangeBucket11to100.Load(),
		RangeBucket11to100Success:    m.RangeBucket11to100Success.Load(),
		RangeBucket101to500:          m.RangeBucket101to500.Load(),
		RangeBucket101to500Success:   m.RangeBucket101to500Success.Load(),
		RangeBucket501to1000:         m.RangeBucket501to1000.Load(),
		RangeBucket501to1000Success:  m.RangeBucket501to1000Success.Load(),
		RangeBucket1001to2000:        m.RangeBucket1001to2000.Load(),
		RangeBucket1001to2000Success: m.RangeBucket1001to2000Success.Load(),
		RangeBucketOver2000:          m.RangeBucketOver2000.Load(),
		RangeBucketOver2000Success:   m.RangeBucketOver2000Success.Load(),

		// Subscriptions
		ActiveSubscriptions: m.ActiveSubscriptions.Load(),
		SubscriptionErrors:  m.SubscriptionErrors.Load(),
	}
}

// MetricsSnapshot represents a point-in-time snapshot of metrics
type MetricsSnapshot struct {
	Timestamp time.Time

	// Worker pool
	TotalWorkers     int32
	ActiveWorkers    int32
	IdleWorkers      int32
	QueueCapacity    int32
	QueueDepth       int32
	QueueUtilization float64
	PeakQueueDepth   int32
	TasksSubmitted   int64
	TasksCompleted   int64
	TasksRejected    int64
	TasksPending     int64
	AvgQueueWaitTime time.Duration
	AvgExecTime      time.Duration

	// DB Semaphore
	DBSemaphoreCapacity int32
	DBSemaphoreInUse    int32
	DBSemaphoreAvail    int32
	AvgDBWaitTime       time.Duration

	// eth_getLogs
	GetLogsTPS       float64
	GetLogsTotal     int64
	GetLogsSuccess   int64
	GetLogsErrors    int64
	GetLogsErrorRate float64
	AvgBlockRange    float64
	PeakBlockRange   int64
	AvgLatency       time.Duration
	MaxLatency       time.Duration

	// Error type breakdown
	ErrRangeTooLarge int64
	ErrTimeout       int64
	ErrRateLimited   int64
	ErrBackpressure  int64
	ErrIOSaturated   int64
	ErrBlockNotFound int64
	ErrQueueFull     int64
	ErrOther         int64

	// Block range distribution (total and success for calculating success rate)
	RangeBucket1to10             int64
	RangeBucket1to10Success      int64
	RangeBucket11to100           int64
	RangeBucket11to100Success    int64
	RangeBucket101to500          int64
	RangeBucket101to500Success   int64
	RangeBucket501to1000         int64
	RangeBucket501to1000Success  int64
	RangeBucket1001to2000        int64
	RangeBucket1001to2000Success int64
	RangeBucketOver2000          int64
	RangeBucketOver2000Success   int64

	// Subscriptions
	ActiveSubscriptions int32
	SubscriptionErrors  int64
}

// PrintMetrics prints current metrics to stdout
func (m *WorkerPoolMetrics) PrintMetrics() {
	s := m.GetSnapshot()

	fmt.Println("\n" + "=" + repeatStr("=", 79))
	fmt.Printf("  EVM RPC METRICS SNAPSHOT - %s\n", s.Timestamp.Format("2006-01-02 15:04:05.000"))
	fmt.Println("=" + repeatStr("=", 79))

	// Worker Pool Section
	fmt.Println("\n┌─ WORKER POOL " + repeatStr("─", 65))
	fmt.Printf("│ Workers:     %d total | %d active | %d idle\n",
		s.TotalWorkers, s.ActiveWorkers, s.IdleWorkers)
	fmt.Printf("│ Queue:       %d/%d (%.1f%% full) | Peak: %d\n",
		s.QueueDepth, s.QueueCapacity, s.QueueUtilization, s.PeakQueueDepth)
	fmt.Printf("│ Tasks:       %d submitted | %d completed | %d rejected | %d pending\n",
		s.TasksSubmitted, s.TasksCompleted, s.TasksRejected, s.TasksPending)
	fmt.Printf("│ Timing:      Avg queue wait: %v | Avg exec: %v\n",
		s.AvgQueueWaitTime.Round(time.Microsecond), s.AvgExecTime.Round(time.Microsecond))

	// DB Semaphore Section
	fmt.Println("├─ DB SEMAPHORE " + repeatStr("─", 63))
	fmt.Printf("│ Capacity:    %d total | %d in-use | %d available\n",
		s.DBSemaphoreCapacity, s.DBSemaphoreInUse, s.DBSemaphoreAvail)
	fmt.Printf("│ Wait Time:   Avg: %v\n", s.AvgDBWaitTime.Round(time.Microsecond))

	// eth_getLogs Section
	fmt.Println("├─ eth_getLogs " + repeatStr("─", 64))
	fmt.Printf("│ Requests:    %d total | %d success | %d errors (%.1f%% error rate)\n",
		s.GetLogsTotal, s.GetLogsSuccess, s.GetLogsErrors, s.GetLogsErrorRate)
	fmt.Printf("│ TPS:         %.2f req/s\n", s.GetLogsTPS)
	fmt.Printf("│ Block Range: Avg: %.1f | Peak: %d\n",
		s.AvgBlockRange, s.PeakBlockRange)
	fmt.Printf("│ Latency:     Avg: %v | Max: %v\n",
		s.AvgLatency.Round(time.Millisecond), s.MaxLatency.Round(time.Millisecond))

	// Error Breakdown Section
	if s.GetLogsErrors > 0 {
		fmt.Println("├─ ERROR BREAKDOWN " + repeatStr("─", 60))
		fmt.Printf("│ Range Too Large:  %d\n", s.ErrRangeTooLarge)
		fmt.Printf("│ Timeout:          %d\n", s.ErrTimeout)
		fmt.Printf("│ Rate Limited:     %d\n", s.ErrRateLimited)
		fmt.Printf("│ Backpressure:     %d\n", s.ErrBackpressure)
		fmt.Printf("│ I/O Saturated:    %d\n", s.ErrIOSaturated)
		fmt.Printf("│ Block Not Found:  %d\n", s.ErrBlockNotFound)
		fmt.Printf("│ Queue Full:       %d\n", s.ErrQueueFull)
		fmt.Printf("│ Other:            %d\n", s.ErrOther)
	}

	// Block Range Distribution Section with success rate
	totalRangeRequests := s.RangeBucket1to10 + s.RangeBucket11to100 + s.RangeBucket101to500 +
		s.RangeBucket501to1000 + s.RangeBucket1001to2000 + s.RangeBucketOver2000
	if totalRangeRequests > 0 {
		fmt.Println("├─ BLOCK RANGE DISTRIBUTION " + repeatStr("─", 51))
		fmt.Println("│ Range            Total    Dist%%   Success  Rate")
		fmt.Println("│ " + repeatStr("─", 50))
		printRangeBucket("1-10 blocks", s.RangeBucket1to10, s.RangeBucket1to10Success, totalRangeRequests)
		printRangeBucket("11-100 blocks", s.RangeBucket11to100, s.RangeBucket11to100Success, totalRangeRequests)
		printRangeBucket("101-500 blocks", s.RangeBucket101to500, s.RangeBucket101to500Success, totalRangeRequests)
		printRangeBucket("501-1000 blocks", s.RangeBucket501to1000, s.RangeBucket501to1000Success, totalRangeRequests)
		printRangeBucket("1001-2000 blocks", s.RangeBucket1001to2000, s.RangeBucket1001to2000Success, totalRangeRequests)
		printRangeBucket(">2000 blocks", s.RangeBucketOver2000, s.RangeBucketOver2000Success, totalRangeRequests)
	}

	// Subscriptions Section
	fmt.Println("├─ SUBSCRIPTIONS " + repeatStr("─", 62))
	fmt.Printf("│ Active:      %d | Errors: %d\n",
		s.ActiveSubscriptions, s.SubscriptionErrors)

	fmt.Println("└" + repeatStr("─", 79))

	// Alert conditions
	if s.QueueUtilization > 80 {
		fmt.Printf("⚠️  WARNING: Queue utilization at %.1f%% - approaching saturation!\n", s.QueueUtilization)
	}
	if s.DBSemaphoreAvail == 0 {
		fmt.Println("⚠️  WARNING: DB Semaphore exhausted - all slots in use!")
	}
	if s.ErrTimeout > 0 && float64(s.ErrTimeout)/float64(max(s.GetLogsTotal, 1)) > 0.1 {
		fmt.Printf("⚠️  WARNING: High timeout rate (%.1f%%) - consider reducing max_blocks_for_log\n",
			float64(s.ErrTimeout)/float64(s.GetLogsTotal)*100)
	}
	if s.RangeBucketOver2000 > 0 {
		fmt.Printf("⚠️  WARNING: %d requests exceeded 2000 block limit\n", s.RangeBucketOver2000)
	}
}

// printRangeBucket prints a single range bucket with success rate
func printRangeBucket(name string, total, success, totalRequests int64) {
	if total == 0 {
		fmt.Printf("│ %-16s %6d   %5.1f%%  %6d   %5.1f%%\n",
			name, total, 0.0, success, 0.0)
	} else {
		successRate := float64(success) / float64(total) * 100
		distPct := float64(total) / float64(totalRequests) * 100
		fmt.Printf("│ %-16s %6d   %5.1f%%  %6d   %5.1f%%\n",
			name, total, distPct, success, successRate)
	}
}

func repeatStr(s string, count int) string {
	result := ""
	for i := 0; i < count; i++ {
		result += s
	}
	return result
}

func must[V any](v V, err error) V {
	if err != nil {
		panic(err)
	}
	return v
}

// ResetMetrics resets all metrics (useful for testing)
func (m *WorkerPoolMetrics) ResetMetrics() {
	m.ActiveWorkers.Store(0)
	m.QueueDepth.Store(0)
	m.PeakQueueDepth.Store(0)
	m.TasksSubmitted.Store(0)
	m.TasksCompleted.Store(0)
	m.TasksRejected.Store(0)
	m.TasksPanicked.Store(0)
	m.TotalWaitTimeNs.Store(0)
	m.TotalExecTimeNs.Store(0)
	m.DBSemaphoreAcquired.Store(0)
	m.DBSemaphoreWaitTimeNs.Store(0)
	m.DBSemaphoreWaitCount.Store(0)
	m.GetLogsRequests.Store(0)
	m.GetLogsErrors.Store(0)
	m.GetLogsSuccess.Store(0)
	m.GetLogsBlockRangeSum.Store(0)
	m.GetLogsLatencySumNs.Store(0)
	m.GetLogsPeakRange.Store(0)
	m.GetLogsMaxLatencyNs.Store(0)
	// Reset error breakdown
	m.ErrRangeTooLarge.Store(0)
	m.ErrTimeout.Store(0)
	m.ErrRateLimited.Store(0)
	m.ErrBackpressure.Store(0)
	m.ErrIOSaturated.Store(0)
	m.ErrBlockNotFound.Store(0)
	m.ErrQueueFull.Store(0)
	m.ErrOther.Store(0)
	// Reset block range buckets
	m.RangeBucket1to10.Store(0)
	m.RangeBucket11to100.Store(0)
	m.RangeBucket101to500.Store(0)
	m.RangeBucket501to1000.Store(0)
	m.RangeBucket1001to2000.Store(0)
	m.RangeBucketOver2000.Store(0)
	// Reset block range success buckets
	m.RangeBucket1to10Success.Store(0)
	m.RangeBucket11to100Success.Store(0)
	m.RangeBucket101to500Success.Store(0)
	m.RangeBucket501to1000Success.Store(0)
	m.RangeBucket1001to2000Success.Store(0)
	m.RangeBucketOver2000Success.Store(0)
	// Reset subscriptions
	m.ActiveSubscriptions.Store(0)
	m.SubscriptionErrors.Store(0)
	m.ResetTPSWindow()
}

// ========================================
// Prometheus Metrics Export Functions
// ========================================

// ExportPrometheusMetrics exports all metrics to OTel
// This should be called periodically (e.g., every 5 seconds)
// All metrics are exported as gauges for efficiency (batch export instead of per-operation)
func (m *WorkerPoolMetrics) ExportPrometheusMetrics() {
	ctx := context.Background()

	// Worker Pool Gauges
	totalWorkers := m.TotalWorkers.Load()
	queueCap := m.QueueCapacity.Load()
	dbSemCap := m.DBSemaphoreCapacity.Load()

	otelMetrics.workersTotal.Record(ctx, int64(totalWorkers))
	otelMetrics.workersActive.Record(ctx, int64(m.ActiveWorkers.Load()))
	otelMetrics.workersIdle.Record(ctx, int64(totalWorkers-m.ActiveWorkers.Load()))
	otelMetrics.queueCapacity.Record(ctx, int64(queueCap))
	otelMetrics.queueDepth.Record(ctx, int64(m.QueueDepth.Load()))
	otelMetrics.queuePeak.Record(ctx, int64(m.PeakQueueDepth.Load()))

	utilization := float64(0)
	if queueCap > 0 {
		utilization = float64(m.QueueDepth.Load()) / float64(queueCap) * 100
	}
	otelMetrics.queueUtilizationPct.Record(ctx, utilization)

	// Task counters (exported as gauges for batch efficiency)
	otelMetrics.tasksSubmittedTotal.Record(ctx, m.TasksSubmitted.Load())
	otelMetrics.tasksCompletedTotal.Record(ctx, m.TasksCompleted.Load())
	otelMetrics.tasksRejectedTotal.Record(ctx, m.TasksRejected.Load())
	otelMetrics.tasksPanickedTotal.Record(ctx, m.TasksPanicked.Load())

	// DB Semaphore Gauges
	otelMetrics.dbSemaphoreCapacity.Record(ctx, int64(dbSemCap))
	otelMetrics.dbSemaphoreInUse.Record(ctx, int64(m.DBSemaphoreAcquired.Load()))
	otelMetrics.dbSemaphoreAvailable.Record(ctx, int64(dbSemCap-m.DBSemaphoreAcquired.Load()))
	otelMetrics.dbSemaphoreWaitCount.Record(ctx, m.DBSemaphoreWaitCount.Load())

	// Subscriptions Gauge
	otelMetrics.subscriptionsActive.Record(ctx, int64(m.ActiveSubscriptions.Load()))

	// eth_getLogs specific gauges
	otelMetrics.getLogsRequestsTotal.Record(ctx, m.GetLogsRequests.Load())
	otelMetrics.getLogsSuccessTotal.Record(ctx, m.GetLogsSuccess.Load())
	otelMetrics.getLogsErrorsTotal.Record(ctx, m.GetLogsErrors.Load())
	otelMetrics.getLogsTPS.Record(ctx, m.GetTPS())
	otelMetrics.getLogsAvgBlockRange.Record(ctx, m.GetAverageBlockRange())
	otelMetrics.getLogsPeakBlockRange.Record(ctx, m.GetLogsPeakRange.Load())
	otelMetrics.getLogsAvgLatencyMs.Record(ctx, float64(m.GetAverageLatency().Milliseconds()))
	otelMetrics.getLogsMaxLatencyMs.Record(ctx, float64(m.GetLogsMaxLatencyNs.Load())/1e6)

	// Error breakdown
	otelMetrics.errRangeTooLarge.Record(ctx, m.ErrRangeTooLarge.Load())
	otelMetrics.errRateLimited.Record(ctx, m.ErrRateLimited.Load())
	otelMetrics.errBackpressure.Record(ctx, m.ErrBackpressure.Load())

	// Average timings
	otelMetrics.avgQueueWaitMs.Record(ctx, float64(m.GetAverageQueueWaitTime().Milliseconds()))
	otelMetrics.avgExecTimeMs.Record(ctx, float64(m.GetAverageExecTime().Milliseconds()))
	otelMetrics.avgDBWaitMs.Record(ctx, float64(m.GetAverageDBWaitTime().Milliseconds()))
}
