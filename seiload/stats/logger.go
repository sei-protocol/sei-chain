package stats

import (
	"context"
	"log"
	"sync"
	"time"
	"github.com/sei-protocol/sei-chain/utils2"
)

// Logger handles periodic statistics logging and dry-run transaction printing
type Logger struct {
	collector *Collector
	interval  time.Duration
	debug     bool

	// Dry-run transaction logging
	txCounter   uint64
	txCounterMu sync.Mutex
}

// NewLogger creates a new statistics logger
func NewLogger(collector *Collector, interval time.Duration, debug bool) *Logger {
	return &Logger{
		collector: collector,
		interval:  interval,
		debug:     debug,
	}
}

// Start begins periodic statistics logging
func (l *Logger) Run(ctx context.Context) error {
	ticker := time.NewTicker(l.interval)
	for {
		if _,err := utils.Recv(ctx, ticker.C); err != nil {
			return err
		}
		l.logCurrentStats()
	}
}

// logCurrentStats logs the current statistics
func (l *Logger) logCurrentStats() {
	stats := l.collector.GetStats()

	// Aggregate metrics for overall summary
	var totalWindowTxs uint64
	var totalTxs uint64
	var totalWindowTPS float64
	var totalCumulativeMaxTPS float64
	var weightedLatencySum time.Duration
	var totalLatencyCount int
	var maxCumulativeLatency time.Duration
	var maxP50, maxP99 time.Duration

	// Log one line per endpoint with concise metrics
	for endpoint, endpointStats := range stats.EndpointStats {
		// Calculate window TPS based on actual window duration
		var windowTPS float64
		if endpointStats.WindowTxCount > 0 {
			// Use the logging interval as the window duration
			windowDuration := l.interval.Seconds()
			windowTPS = float64(endpointStats.WindowTxCount) / windowDuration
		}

		// Calculate window average latency
		var windowAvgLatency time.Duration
		if endpointStats.WindowLatencyCount > 0 {
			windowAvgLatency = endpointStats.WindowLatencySum / time.Duration(endpointStats.WindowLatencyCount)
		}

		// Get total transactions for this endpoint
		totalTxsForEndpoint := uint64(0)
		for _, endpoints := range stats.TxCounts {
			if count, exists := endpoints[endpoint]; exists {
				totalTxsForEndpoint += count
			}
		}

		// Aggregate for overall summary
		totalWindowTxs += endpointStats.WindowTxCount
		totalTxs += totalTxsForEndpoint
		totalWindowTPS += windowTPS
		totalCumulativeMaxTPS += endpointStats.CumulativeMaxTPS
		weightedLatencySum += endpointStats.WindowLatencySum
		totalLatencyCount += endpointStats.WindowLatencyCount
		if endpointStats.CumulativeMaxLatency > maxCumulativeLatency {
			maxCumulativeLatency = endpointStats.CumulativeMaxLatency
		}
		if endpointStats.P50Latency > maxP50 {
			maxP50 = endpointStats.P50Latency
		}
		if endpointStats.P99Latency > maxP99 {
			maxP99 = endpointStats.P99Latency
		}

		if l.debug {
			// Format: [timestamp] endpoint | TXs: total | TPS: window(max) | Latency: avg(max) | P50: x P99: x
			log.Printf("[%s] %s | TXs: %d | TPS: %.1f(%.1f) | Lat: %v(%v) | P50: %v P99: %v\n",
				time.Now().Format("15:04:05"),
				endpoint,
				totalTxsForEndpoint,
				windowTPS,
				endpointStats.CumulativeMaxTPS,
				windowAvgLatency.Round(time.Millisecond),
				endpointStats.CumulativeMaxLatency.Round(time.Millisecond),
				endpointStats.P50Latency.Round(time.Millisecond),
				endpointStats.P99Latency.Round(time.Millisecond))
		}
	}

	// Calculate overall average latency
	var overallAvgLatency time.Duration
	if totalLatencyCount > 0 {
		overallAvgLatency = weightedLatencySum / time.Duration(totalLatencyCount)
	}

	// Print overall summary line
	log.Printf("[%s] throughput tps=%.2f, txs=%d,  latency(avg=%v p50=%v p99=%v max=%v)\n",
		time.Now().Format("15:04:05"),
		totalWindowTPS,
		totalTxs,
		overallAvgLatency.Round(time.Millisecond),
		maxP50.Round(time.Millisecond),
		maxP99.Round(time.Millisecond),
		maxCumulativeLatency.Round(time.Millisecond))

	// Print block statistics if available
	if stats.BlockStats != nil && stats.BlockStats.SampleCount > 0 {
		log.Printf("[%s] %s\n",
			time.Now().Format("15:04:05"),
			stats.BlockStats.FormatBlockStats())
	}

	// Reset window stats for next period
	l.collector.ResetWindowStats()

	// Reset block collector window stats
	if blockCollector := l.collector.GetBlockCollector(); blockCollector != nil {
		blockCollector.ResetWindowStats()
	}
}

// LogFinalStats logs comprehensive final statistics
func (l *Logger) LogFinalStats() {
	stats := l.collector.GetStats()

	log.Print("\n=============================")
	log.Print("FINAL LOAD TEST RESULTS")
	log.Print("=============================")
	log.Print(stats.FormatStats())

	// Additional final statistics
	duration := time.Since(stats.StartTime)
	if duration.Seconds() > 0 {
		log.Printf("\nOverall Performance Summary:")
		log.Printf("  Total Runtime: %v", duration.Round(time.Second))
		log.Printf("  Total Transactions: %d", stats.TotalTxs)
		log.Printf("  Average TPS: %.2f", float64(stats.TotalTxs)/duration.Seconds())
		log.Printf("  Max TPS: %.2f", stats.OverallMaxTPS)

		// Calculate total transactions per scenario
		scenarioTotals := make(map[string]uint64)
		for scenario, endpoints := range stats.TxCounts {
			total := uint64(0)
			for _, count := range endpoints {
				total += count
			}
			scenarioTotals[scenario] = total
		}

		log.Printf("\nScenario Distribution:")
		for scenario, total := range scenarioTotals {
			percentage := float64(total) / float64(stats.TotalTxs) * 100
			log.Printf("  %s: %d (%.1f%%)", scenario, total, percentage)
		}
	}

	// Print overall gas statistics if available (use cumulative data)
	if cumulativeBlockStats := l.collector.GetCumulativeBlockStats(); cumulativeBlockStats != nil && cumulativeBlockStats.SampleCount > 0 {
		log.Printf("\nOverall Gas Statistics:")
		log.Printf("  Max Block Number: %d", cumulativeBlockStats.MaxBlockNumber)
		log.Printf("  Block Times: p50=%v p99=%v max=%v",
			cumulativeBlockStats.P50BlockTime.Round(time.Millisecond),
			cumulativeBlockStats.P99BlockTime.Round(time.Millisecond),
			cumulativeBlockStats.MaxBlockTime.Round(time.Millisecond))
		log.Printf("  Gas Usage: p50=%d p99=%d max=%d",
			cumulativeBlockStats.P50GasUsed,
			cumulativeBlockStats.P99GasUsed,
			cumulativeBlockStats.MaxGasUsed)
		log.Printf("  Block Samples: %d", cumulativeBlockStats.SampleCount)
	}

	log.Printf("==============================")
}
