package evmrpc

import (
	"fmt"
	"sync"
	"sync/atomic"
	"time"

	gometrics "github.com/armon/go-metrics"
	"github.com/cosmos/cosmos-sdk/telemetry"
)

// WorkerPoolMetrics tracks worker pool performance metrics
type WorkerPoolMetrics struct {
	// Worker pool stats
	TotalWorkers    int32
	ActiveWorkers   atomic.Int32
	QueueCapacity   int32
	QueueDepth      atomic.Int32
	PeakQueueDepth  atomic.Int32
	TasksSubmitted  atomic.Int64
	TasksCompleted  atomic.Int64
	TasksRejected   atomic.Int64 // Queue full rejections
	TasksPanicked   atomic.Int64
	TotalWaitTimeNs atomic.Int64 // Total time tasks spent waiting in queue
	TotalExecTimeNs atomic.Int64 // Total task execution time

	// DB Semaphore stats
	DBSemaphoreCapacity   int32
	DBSemaphoreAcquired   atomic.Int32
	DBSemaphoreWaitTimeNs atomic.Int64
	DBSemaphoreWaitCount  atomic.Int64

	// eth_getLogs specific stats
	GetLogsRequests      atomic.Int64
	GetLogsErrors        atomic.Int64
	GetLogsBlockRangeSum atomic.Int64 // Sum of block ranges for average calculation
	GetLogsLatencySumNs  atomic.Int64 // Sum of latencies for average calculation
	GetLogsPeakRange     atomic.Int64

	// Subscription stats
	ActiveSubscriptions atomic.Int32
	SubscriptionErrors  atomic.Int64

	// Time window for TPS calculation
	windowStart    time.Time
	windowRequests atomic.Int64
	mu             sync.RWMutex
}

var (
	globalMetrics      *WorkerPoolMetrics
	globalMetricsOnce  sync.Once
	metricsPrinterOnce sync.Once
	metricsStopChan    chan struct{}
)

// InitGlobalMetrics initializes the global metrics instance
func InitGlobalMetrics(workerCount, queueCapacity, dbSemaphoreCapacity int) *WorkerPoolMetrics {
	globalMetricsOnce.Do(func() {
		globalMetrics = &WorkerPoolMetrics{
			TotalWorkers:        int32(workerCount),
			QueueCapacity:       int32(queueCapacity),
			DBSemaphoreCapacity: int32(dbSemaphoreCapacity),
			windowStart:         time.Now(),
		}
	})
	return globalMetrics
}

// GetGlobalMetrics returns the global metrics instance
func GetGlobalMetrics() *WorkerPoolMetrics {
	if globalMetrics == nil {
		// Initialize with defaults if not already done
		InitGlobalMetrics(MaxWorkerPoolSize, DefaultWorkerQueueSize, MaxDBReadConcurrency)
	}
	return globalMetrics
}

// StartMetricsPrinter starts a background goroutine that prints metrics every interval
// This is idempotent - only the first call will start the printer
func StartMetricsPrinter(interval time.Duration) {
	metricsPrinterOnce.Do(func() {
		metricsStopChan = make(chan struct{})
		go func() {
			ticker := time.NewTicker(interval)
			defer ticker.Stop()

			for {
				select {
				case <-ticker.C:
					m := GetGlobalMetrics()
					// Export to Prometheus (gauges need periodic update)
					m.ExportPrometheusMetrics()
					// Print to stdout
					m.PrintMetrics()
				case <-metricsStopChan:
					return
				}
			}
		}()
	})
}

// StopMetricsPrinter stops the metrics printer
func StopMetricsPrinter() {
	if metricsStopChan != nil {
		close(metricsStopChan)
	}
}

// RecordTaskSubmitted records a task submission
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
	// Export to Prometheus
	IncrPrometheusTaskSubmitted()
}

// RecordTaskStarted records when a task starts executing
func (m *WorkerPoolMetrics) RecordTaskStarted(queuedAt time.Time) {
	m.ActiveWorkers.Add(1)
	m.QueueDepth.Add(-1)
	waitTime := time.Since(queuedAt)
	m.TotalWaitTimeNs.Add(waitTime.Nanoseconds())
	// Export to Prometheus
	RecordPrometheusQueueWait(waitTime)
}

// RecordTaskCompleted records a task completion
func (m *WorkerPoolMetrics) RecordTaskCompleted(startedAt time.Time) {
	m.ActiveWorkers.Add(-1)
	m.TasksCompleted.Add(1)
	execTime := time.Since(startedAt)
	m.TotalExecTimeNs.Add(execTime.Nanoseconds())
	// Export to Prometheus
	IncrPrometheusTaskCompleted()
	RecordPrometheusTaskExec(execTime)
}

// RecordTaskRejected records a task rejection (queue full)
func (m *WorkerPoolMetrics) RecordTaskRejected() {
	m.TasksRejected.Add(1)
	// Export to Prometheus
	IncrPrometheusTaskRejected()
}

// RecordTaskPanicked records a task panic
func (m *WorkerPoolMetrics) RecordTaskPanicked() {
	m.TasksPanicked.Add(1)
	// Export to Prometheus
	IncrPrometheusTaskPanicked()
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
func (m *WorkerPoolMetrics) RecordDBSemaphoreWait(waitTime time.Duration) {
	m.DBSemaphoreWaitTimeNs.Add(waitTime.Nanoseconds())
	m.DBSemaphoreWaitCount.Add(1)
	// Export to Prometheus
	RecordPrometheusDBSemaphoreWait(waitTime)
}

// RecordGetLogsRequest records an eth_getLogs request
func (m *WorkerPoolMetrics) RecordGetLogsRequest(blockRange int64, latency time.Duration, startTime time.Time, err error) {
	m.GetLogsRequests.Add(1)
	m.windowRequests.Add(1)
	m.GetLogsBlockRangeSum.Add(blockRange)
	m.GetLogsLatencySumNs.Add(latency.Nanoseconds())

	// Update peak range
	for {
		peak := m.GetLogsPeakRange.Load()
		if blockRange <= peak || m.GetLogsPeakRange.CompareAndSwap(peak, blockRange) {
			break
		}
	}

	if err != nil {
		m.GetLogsErrors.Add(1)
	}

	// Export to Prometheus
	IncrPrometheusGetLogsRequest(err == nil, blockRange)
	MeasurePrometheusGetLogsLatency(startTime, blockRange)
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
	IncrPrometheusSubscriptionError()
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
	return MetricsSnapshot{
		Timestamp: time.Now(),

		// Worker pool
		TotalWorkers:     m.TotalWorkers,
		ActiveWorkers:    m.ActiveWorkers.Load(),
		IdleWorkers:      m.TotalWorkers - m.ActiveWorkers.Load(),
		QueueCapacity:    m.QueueCapacity,
		QueueDepth:       m.QueueDepth.Load(),
		QueueUtilization: float64(m.QueueDepth.Load()) / float64(m.QueueCapacity) * 100,
		PeakQueueDepth:   m.PeakQueueDepth.Load(),
		TasksSubmitted:   m.TasksSubmitted.Load(),
		TasksCompleted:   m.TasksCompleted.Load(),
		TasksRejected:    m.TasksRejected.Load(),
		TasksPending:     m.TasksSubmitted.Load() - m.TasksCompleted.Load() - m.TasksRejected.Load(),
		AvgQueueWaitTime: m.GetAverageQueueWaitTime(),
		AvgExecTime:      m.GetAverageExecTime(),

		// DB Semaphore
		DBSemaphoreCapacity: m.DBSemaphoreCapacity,
		DBSemaphoreInUse:    m.DBSemaphoreAcquired.Load(),
		DBSemaphoreAvail:    m.DBSemaphoreCapacity - m.DBSemaphoreAcquired.Load(),
		AvgDBWaitTime:       m.GetAverageDBWaitTime(),

		// eth_getLogs
		GetLogsTPS:       m.GetTPS(),
		GetLogsTotal:     m.GetLogsRequests.Load(),
		GetLogsErrors:    m.GetLogsErrors.Load(),
		GetLogsErrorRate: float64(m.GetLogsErrors.Load()) / float64(max(m.GetLogsRequests.Load(), 1)) * 100,
		AvgBlockRange:    m.GetAverageBlockRange(),
		PeakBlockRange:   m.GetLogsPeakRange.Load(),
		AvgLatency:       m.GetAverageLatency(),

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
	GetLogsErrors    int64
	GetLogsErrorRate float64
	AvgBlockRange    float64
	PeakBlockRange   int64
	AvgLatency       time.Duration

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
	fmt.Printf("│ Requests:    %d total | %.2f TPS | %d errors (%.1f%% error rate)\n",
		s.GetLogsTotal, s.GetLogsTPS, s.GetLogsErrors, s.GetLogsErrorRate)
	fmt.Printf("│ Block Range: Avg: %.1f | Peak: %d\n",
		s.AvgBlockRange, s.PeakBlockRange)
	fmt.Printf("│ Latency:     Avg: %v\n", s.AvgLatency.Round(time.Millisecond))

	// Subscriptions Section
	fmt.Println("├─ SUBSCRIPTIONS " + repeatStr("─", 62))
	fmt.Printf("│ Active:      %d | Errors: %d\n",
		s.ActiveSubscriptions, s.SubscriptionErrors)

	fmt.Println("└" + repeatStr("─", 79))

	// Alert conditions
	if s.QueueUtilization > 80 {
		fmt.Printf("⚠️  WARNING: Queue utilization at %.1f%% - approaching saturation!\n", s.QueueUtilization)
	}
	if s.GetLogsErrorRate > 5 {
		fmt.Printf("⚠️  WARNING: eth_getLogs error rate at %.1f%%!\n", s.GetLogsErrorRate)
	}
	if s.DBSemaphoreAvail == 0 {
		fmt.Println("⚠️  WARNING: DB Semaphore exhausted - all slots in use!")
	}
}

func repeatStr(s string, count int) string {
	result := ""
	for i := 0; i < count; i++ {
		result += s
	}
	return result
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
	m.GetLogsBlockRangeSum.Store(0)
	m.GetLogsLatencySumNs.Store(0)
	m.GetLogsPeakRange.Store(0)
	m.ActiveSubscriptions.Store(0)
	m.SubscriptionErrors.Store(0)
	m.ResetTPSWindow()
}

// ========================================
// Prometheus Metrics Export Functions
// ========================================

// ExportPrometheusMetrics exports all metrics to Prometheus
// This should be called periodically (e.g., every 5 seconds)
func (m *WorkerPoolMetrics) ExportPrometheusMetrics() {
	// Worker Pool Gauges
	gometrics.SetGauge([]string{"sei", "evm", "workerpool", "workers", "total"}, float32(m.TotalWorkers))
	gometrics.SetGauge([]string{"sei", "evm", "workerpool", "workers", "active"}, float32(m.ActiveWorkers.Load()))
	gometrics.SetGauge([]string{"sei", "evm", "workerpool", "workers", "idle"}, float32(m.TotalWorkers-m.ActiveWorkers.Load()))
	gometrics.SetGauge([]string{"sei", "evm", "workerpool", "queue", "capacity"}, float32(m.QueueCapacity))
	gometrics.SetGauge([]string{"sei", "evm", "workerpool", "queue", "depth"}, float32(m.QueueDepth.Load()))
	gometrics.SetGauge([]string{"sei", "evm", "workerpool", "queue", "peak"}, float32(m.PeakQueueDepth.Load()))

	// Queue utilization percentage
	utilization := float32(0)
	if m.QueueCapacity > 0 {
		utilization = float32(m.QueueDepth.Load()) / float32(m.QueueCapacity) * 100
	}
	gometrics.SetGauge([]string{"sei", "evm", "workerpool", "queue", "utilization"}, utilization)

	// DB Semaphore Gauges
	gometrics.SetGauge([]string{"sei", "evm", "db", "semaphore", "capacity"}, float32(m.DBSemaphoreCapacity))
	gometrics.SetGauge([]string{"sei", "evm", "db", "semaphore", "inuse"}, float32(m.DBSemaphoreAcquired.Load()))
	gometrics.SetGauge([]string{"sei", "evm", "db", "semaphore", "available"}, float32(m.DBSemaphoreCapacity-m.DBSemaphoreAcquired.Load()))

	// Subscriptions Gauge
	gometrics.SetGauge([]string{"sei", "evm", "subscriptions", "active"}, float32(m.ActiveSubscriptions.Load()))

	// eth_getLogs specific gauges
	gometrics.SetGauge([]string{"sei", "evm", "getlogs", "tps"}, float32(m.GetTPS()))
	gometrics.SetGauge([]string{"sei", "evm", "getlogs", "avg", "blockrange"}, float32(m.GetAverageBlockRange()))
	gometrics.SetGauge([]string{"sei", "evm", "getlogs", "peak", "blockrange"}, float32(m.GetLogsPeakRange.Load()))
	gometrics.SetGauge([]string{"sei", "evm", "getlogs", "avg", "latency", "ms"}, float32(m.GetAverageLatency().Milliseconds()))

	// Average timings
	gometrics.SetGauge([]string{"sei", "evm", "workerpool", "avg", "queue", "wait", "ms"}, float32(m.GetAverageQueueWaitTime().Milliseconds()))
	gometrics.SetGauge([]string{"sei", "evm", "workerpool", "avg", "exec", "time", "ms"}, float32(m.GetAverageExecTime().Milliseconds()))
	gometrics.SetGauge([]string{"sei", "evm", "db", "semaphore", "avg", "wait", "ms"}, float32(m.GetAverageDBWaitTime().Milliseconds()))
}

// IncrTaskSubmitted increments task submitted counter in Prometheus
func IncrPrometheusTaskSubmitted() {
	telemetry.IncrCounter(1, "sei", "evm", "workerpool", "tasks", "submitted")
}

// IncrTaskCompleted increments task completed counter in Prometheus
func IncrPrometheusTaskCompleted() {
	telemetry.IncrCounter(1, "sei", "evm", "workerpool", "tasks", "completed")
}

// IncrTaskRejected increments task rejected counter in Prometheus
func IncrPrometheusTaskRejected() {
	telemetry.IncrCounterWithLabels(
		[]string{"sei", "evm", "workerpool", "tasks", "rejected"},
		1,
		[]gometrics.Label{telemetry.NewLabel("reason", "queue_full")},
	)
}

// IncrTaskPanicked increments task panicked counter in Prometheus
func IncrPrometheusTaskPanicked() {
	telemetry.IncrCounter(1, "sei", "evm", "workerpool", "tasks", "panicked")
}

// IncrGetLogsRequest increments eth_getLogs request counter with labels
func IncrPrometheusGetLogsRequest(success bool, blockRange int64) {
	rangeLabel := "small"
	if blockRange > 100 {
		rangeLabel = "medium"
	}
	if blockRange > 1000 {
		rangeLabel = "large"
	}

	successStr := "true"
	if !success {
		successStr = "false"
	}

	telemetry.IncrCounterWithLabels(
		[]string{"sei", "evm", "getlogs", "requests"},
		1,
		[]gometrics.Label{
			telemetry.NewLabel("success", successStr),
			telemetry.NewLabel("range", rangeLabel),
		},
	)
}

// MeasureGetLogsLatency records eth_getLogs latency
func MeasurePrometheusGetLogsLatency(startTime time.Time, blockRange int64) {
	rangeLabel := "small"
	if blockRange > 100 {
		rangeLabel = "medium"
	}
	if blockRange > 1000 {
		rangeLabel = "large"
	}

	gometrics.MeasureSinceWithLabels(
		[]string{"sei", "evm", "getlogs", "latency", "ms"},
		startTime.UTC(),
		[]gometrics.Label{telemetry.NewLabel("range", rangeLabel)},
	)
}

// IncrSubscriptionError increments subscription error counter
func IncrPrometheusSubscriptionError() {
	telemetry.IncrCounter(1, "sei", "evm", "subscriptions", "errors")
}

// RecordDBSemaphoreWaitTime records DB semaphore wait time histogram
func RecordPrometheusDBSemaphoreWait(waitTime time.Duration) {
	gometrics.AddSample(
		[]string{"sei", "evm", "db", "semaphore", "wait", "ms"},
		float32(waitTime.Milliseconds()),
	)
}

// RecordQueueWaitTime records queue wait time histogram
func RecordPrometheusQueueWait(waitTime time.Duration) {
	gometrics.AddSample(
		[]string{"sei", "evm", "workerpool", "queue", "wait", "ms"},
		float32(waitTime.Milliseconds()),
	)
}

// RecordTaskExecTime records task execution time histogram
func RecordPrometheusTaskExec(execTime time.Duration) {
	gometrics.AddSample(
		[]string{"sei", "evm", "workerpool", "task", "exec", "ms"},
		float32(execTime.Milliseconds()),
	)
}
