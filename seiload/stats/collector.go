package stats

import (
	"fmt"
	"sort"
	"sync"
	"time"
)

// Collector tracks comprehensive statistics for load testing
type Collector struct {
	mu sync.RWMutex

	// Transaction counts by scenario and endpoint
	txCounts map[string]map[string]uint64 // [scenario][endpoint] -> count

	// Latency tracking per endpoint
	latencies map[string][]time.Duration // [endpoint] -> []latency

	// TPS tracking with 10-second windows
	tpsWindows map[string]*TPSWindow // [endpoint] -> TPS window

	// Overall TPS tracking across all endpoints
	overallTpsWindow *TPSWindow

	// Window-based tracking for periodic reporting
	windowStats map[string]*WindowStats // [endpoint] -> window stats

	// Global metrics
	startTime time.Time
	totalTxs  uint64
	lastWindowTime time.Time

	// Configuration
	maxLatencyHistory int // Limit latency history to prevent memory leaks
}

// TPSWindow tracks transactions in a sliding 10-second window
type TPSWindow struct {
	timestamps []time.Time
	maxTPS     float64
	mu         sync.RWMutex
}

// NewCollector creates a new statistics collector
func NewCollector() *Collector {
	return &Collector{
		txCounts:          make(map[string]map[string]uint64),
		latencies:         make(map[string][]time.Duration),
		tpsWindows:        make(map[string]*TPSWindow),
		windowStats:       make(map[string]*WindowStats),
		overallTpsWindow:  &TPSWindow{timestamps: make([]time.Time, 0)},
		startTime:         time.Now(),
		lastWindowTime:    time.Now(),
		maxLatencyHistory: 10000, // Keep last 10k latencies per endpoint
	}
}

// RecordTransaction records a transaction attempt
func (c *Collector) RecordTransaction(scenario, endpoint string, latency time.Duration, success bool) {
	c.mu.Lock()
	defer c.mu.Unlock()

	// Initialize maps if needed
	if c.txCounts[scenario] == nil {
		c.txCounts[scenario] = make(map[string]uint64)
	}
	if c.latencies[endpoint] == nil {
		c.latencies[endpoint] = make([]time.Duration, 0)
	}
	if c.tpsWindows[endpoint] == nil {
		c.tpsWindows[endpoint] = &TPSWindow{
			timestamps: make([]time.Time, 0),
		}
	}
	if c.windowStats[endpoint] == nil {
		c.windowStats[endpoint] = &WindowStats{
			windowStart: time.Now(),
		}
	}

	// Record transaction count
	c.txCounts[scenario][endpoint]++
	c.totalTxs++

	// Record latency (only for successful transactions)
	if success {
		c.recordLatency(endpoint, latency)
	}

	// Record TPS
	c.recordTPS(endpoint)
	c.recordOverallTPS()

	// Record window stats
	c.recordWindowStats(endpoint, latency)
}

// recordLatency adds a latency measurement, maintaining history limit
func (c *Collector) recordLatency(endpoint string, latency time.Duration) {
	latencyList := c.latencies[endpoint]

	// Add new latency
	latencyList = append(latencyList, latency)

	// Trim if over limit (keep most recent)
	if len(latencyList) > c.maxLatencyHistory {
		latencyList = latencyList[len(latencyList)-c.maxLatencyHistory:]
	}

	c.latencies[endpoint] = latencyList
}

// recordTPS updates the TPS window for an endpoint
func (c *Collector) recordTPS(endpoint string) {
	window := c.tpsWindows[endpoint]
	window.mu.Lock()
	defer window.mu.Unlock()

	now := time.Now()
	window.timestamps = append(window.timestamps, now)

	// Remove timestamps older than 10 seconds
	cutoff := now.Add(-10 * time.Second)
	validIndex := 0
	for i, ts := range window.timestamps {
		if ts.After(cutoff) {
			validIndex = i
			break
		}
	}
	window.timestamps = window.timestamps[validIndex:]

	// Calculate current TPS and update max
	currentTPS := float64(len(window.timestamps)) / 10.0
	if currentTPS > window.maxTPS {
		window.maxTPS = currentTPS
	}
}

// recordOverallTPS updates the overall TPS window
func (c *Collector) recordOverallTPS() {
	now := time.Now()
	c.overallTpsWindow.mu.Lock()
	defer c.overallTpsWindow.mu.Unlock()

	c.overallTpsWindow.timestamps = append(c.overallTpsWindow.timestamps, now)

	// Remove timestamps older than 10 seconds
	cutoff := now.Add(-10 * time.Second)
	validIndex := 0
	for i, ts := range c.overallTpsWindow.timestamps {
		if ts.After(cutoff) {
			validIndex = i
			break
		}
	}
	c.overallTpsWindow.timestamps = c.overallTpsWindow.timestamps[validIndex:]

	// Calculate current TPS and update max
	currentTPS := float64(len(c.overallTpsWindow.timestamps)) / 10.0
	if currentTPS > c.overallTpsWindow.maxTPS {
		c.overallTpsWindow.maxTPS = currentTPS
	}
}

// recordWindowStats updates the window stats for an endpoint
func (c *Collector) recordWindowStats(endpoint string, latency time.Duration) {
	windowStats := c.windowStats[endpoint]

	// Update tx count
	windowStats.txCount++

	// Update latency sum and count
	windowStats.latencySum += latency
	windowStats.latencyCount++

	// Update max and min latency
	if latency > windowStats.maxLatency {
		windowStats.maxLatency = latency
	}
	if latency < windowStats.minLatency || windowStats.minLatency == 0 {
		windowStats.minLatency = latency
	}

	// Update cumulative max TPS and latency
	if windowStats.txCount > 0 {
		currentTPS := float64(windowStats.txCount) / time.Since(windowStats.windowStart).Seconds()
		if currentTPS > windowStats.cumulativeMaxTPS {
			windowStats.cumulativeMaxTPS = currentTPS
		}
	}
	if latency > windowStats.cumulativeMaxLatency {
		windowStats.cumulativeMaxLatency = latency
	}
}

// ResetWindowStats resets the window statistics for all endpoints
func (c *Collector) ResetWindowStats() {
	c.mu.Lock()
	defer c.mu.Unlock()

	now := time.Now()
	for endpoint, windowStats := range c.windowStats {
		// Preserve cumulative maximums
		cumulativeMaxTPS := windowStats.cumulativeMaxTPS
		cumulativeMaxLatency := windowStats.cumulativeMaxLatency

		// Reset window stats
		c.windowStats[endpoint] = &WindowStats{
			windowStart:          now,
			cumulativeMaxTPS:     cumulativeMaxTPS,
			cumulativeMaxLatency: cumulativeMaxLatency,
		}
	}
	c.lastWindowTime = now
}

// GetStats returns comprehensive statistics
func (c *Collector) GetStats() Stats {
	c.mu.RLock()
	defer c.mu.RUnlock()

	stats := Stats{
		StartTime:     c.startTime,
		TotalTxs:      c.totalTxs,
		TxCounts:      make(map[string]map[string]uint64),
		EndpointStats: make(map[string]EndpointStats),
	}

	// Copy transaction counts
	for scenario, endpoints := range c.txCounts {
		stats.TxCounts[scenario] = make(map[string]uint64)
		for endpoint, count := range endpoints {
			stats.TxCounts[scenario][endpoint] = count
		}
	}

	// Calculate endpoint statistics
	for endpoint, latencyList := range c.latencies {
		endpointStats := EndpointStats{
			Endpoint: endpoint,
		}

		// Calculate latency percentiles
		if len(latencyList) > 0 {
			sortedLatencies := make([]time.Duration, len(latencyList))
			copy(sortedLatencies, latencyList)
			sort.Slice(sortedLatencies, func(i, j int) bool {
				return sortedLatencies[i] < sortedLatencies[j]
			})

			endpointStats.P50Latency = calculatePercentile(sortedLatencies, 50)
			endpointStats.P99Latency = calculatePercentile(sortedLatencies, 99)
			endpointStats.SampleCount = len(sortedLatencies)
		}

		// Get TPS data
		if window := c.tpsWindows[endpoint]; window != nil {
			window.mu.RLock()
			endpointStats.MaxTPS = window.maxTPS
			// Calculate current TPS
			now := time.Now()
			cutoff := now.Add(-10 * time.Second)
			currentCount := 0
			for _, ts := range window.timestamps {
				if ts.After(cutoff) {
					currentCount++
				}
			}
			endpointStats.CurrentTPS = float64(currentCount) / 10.0
			window.mu.RUnlock()
		}

		// Get window stats
		if windowStats := c.windowStats[endpoint]; windowStats != nil {
			endpointStats.WindowTxCount = windowStats.txCount
			endpointStats.WindowLatencySum = windowStats.latencySum
			endpointStats.WindowLatencyCount = windowStats.latencyCount
			endpointStats.WindowMaxLatency = windowStats.maxLatency
			endpointStats.WindowMinLatency = windowStats.minLatency
			endpointStats.CumulativeMaxTPS = windowStats.cumulativeMaxTPS
			endpointStats.CumulativeMaxLatency = windowStats.cumulativeMaxLatency
		}

		stats.EndpointStats[endpoint] = endpointStats
	}

	// Get overall TPS
	c.overallTpsWindow.mu.RLock()
	stats.OverallMaxTPS = c.overallTpsWindow.maxTPS
	now := time.Now()
	cutoff := now.Add(-10 * time.Second)
	currentCount := 0
	for _, ts := range c.overallTpsWindow.timestamps {
		if ts.After(cutoff) {
			currentCount++
		}
	}
	stats.OverallCurrentTPS = float64(currentCount) / 10.0
	c.overallTpsWindow.mu.RUnlock()

	return stats
}

// calculatePercentile calculates the given percentile from sorted durations
func calculatePercentile(sorted []time.Duration, percentile int) time.Duration {
	if len(sorted) == 0 {
		return 0
	}

	index := (len(sorted) * percentile) / 100
	if index >= len(sorted) {
		index = len(sorted) - 1
	}

	return sorted[index]
}

// Stats represents comprehensive load test statistics
type Stats struct {
	StartTime     time.Time
	TotalTxs      uint64
	TxCounts      map[string]map[string]uint64 // [scenario][endpoint] -> count
	EndpointStats map[string]EndpointStats
	OverallMaxTPS float64
	OverallCurrentTPS float64
}

// EndpointStats represents statistics for a specific endpoint
type EndpointStats struct {
	Endpoint    string
	P50Latency  time.Duration
	P99Latency  time.Duration
	MaxTPS      float64
	CurrentTPS  float64
	SampleCount int
	QueueDepth  int // Current queue depth for monitoring backpressure

	// Window stats
	WindowTxCount        uint64
	WindowLatencySum     time.Duration
	WindowLatencyCount   int
	WindowMaxLatency     time.Duration
	WindowMinLatency     time.Duration
	CumulativeMaxTPS     float64
	CumulativeMaxLatency time.Duration
}

// WindowStats tracks metrics for the current reporting window
type WindowStats struct {
	windowStart    time.Time
	txCount        uint64
	latencySum     time.Duration
	latencyCount   int
	maxLatency     time.Duration
	minLatency     time.Duration
	
	// Cumulative maximums
	cumulativeMaxTPS     float64
	cumulativeMaxLatency time.Duration
}

// FormatStats returns a formatted string representation of the statistics
func (s *Stats) FormatStats() string {
	duration := time.Since(s.StartTime)
	avgTPS := float64(s.TotalTxs) / duration.Seconds()

	result := fmt.Sprintf("\n=== Load Test Statistics ===\n")
	result += fmt.Sprintf("Runtime: %v | Total TXs: %d | Avg TPS: %.2f\n\n",
		duration.Round(time.Second), s.TotalTxs, avgTPS)

	// Transaction counts by scenario
	result += "Transaction Counts by Scenario:\n"
	for scenario, endpoints := range s.TxCounts {
		result += fmt.Sprintf("  %s:\n", scenario)
		for endpoint, count := range endpoints {
			result += fmt.Sprintf("    %s: %d\n", endpoint, count)
		}
	}

	// Endpoint statistics
	result += "\nEndpoint Performance:\n"
	for endpoint, stats := range s.EndpointStats {
		result += fmt.Sprintf("  %s:\n", endpoint)
		result += fmt.Sprintf("    Latency P50: %v | P99: %v (samples: %d)\n",
			stats.P50Latency.Round(time.Millisecond),
			stats.P99Latency.Round(time.Millisecond),
			stats.SampleCount)
		result += fmt.Sprintf("    TPS Current: %.2f | Max (10s): %.2f\n",
			stats.CurrentTPS, stats.MaxTPS)

		// Window stats
		result += fmt.Sprintf("    Window TXs: %d | Latency Sum: %v | Latency Count: %d\n",
			stats.WindowTxCount, stats.WindowLatencySum.Round(time.Millisecond), stats.WindowLatencyCount)
		result += fmt.Sprintf("    Window Max Latency: %v | Window Min Latency: %v\n",
			stats.WindowMaxLatency.Round(time.Millisecond), stats.WindowMinLatency.Round(time.Millisecond))
		result += fmt.Sprintf("    Cumulative Max TPS: %.2f | Cumulative Max Latency: %v\n",
			stats.CumulativeMaxTPS, stats.CumulativeMaxLatency.Round(time.Millisecond))
	}

	// Overall TPS
	result += fmt.Sprintf("\nOverall TPS: Current: %.2f | Max (10s): %.2f\n",
		s.OverallCurrentTPS, s.OverallMaxTPS)

	return result
}
