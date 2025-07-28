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

	// Global metrics
	startTime time.Time
	totalTxs  uint64

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
		startTime:         time.Now(),
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

	// Record transaction count
	c.txCounts[scenario][endpoint]++
	c.totalTxs++

	// Record latency (only for successful transactions)
	if success {
		c.recordLatency(endpoint, latency)
	}

	// Record TPS
	c.recordTPS(endpoint)
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

		stats.EndpointStats[endpoint] = endpointStats
	}

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
	}

	return result
}
