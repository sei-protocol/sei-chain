package evmrpc

import (
	"fmt"
	"os"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	gometrics "github.com/armon/go-metrics"
	"github.com/cosmos/cosmos-sdk/telemetry"
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
		// DB semaphore is aligned with worker count
		InitGlobalMetrics(MaxWorkerPoolSize, DefaultWorkerQueueSize, MaxWorkerPoolSize)
	}
	return globalMetrics
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
			defer ticker.Stop()

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

	// Export to Prometheus
	IncrPrometheusGetLogsRequest(err == nil, blockRange)
	MeasurePrometheusGetLogsLatency(startTime, blockRange)
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
