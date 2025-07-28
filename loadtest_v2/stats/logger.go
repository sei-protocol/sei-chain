package stats

import (
	"context"
	"fmt"
	"log"
	"sync"
	"time"

	"github.com/sei-protocol/sei-chain/loadtest_v2/types"
)

// Logger handles periodic statistics logging and dry-run transaction printing
type Logger struct {
	collector *Collector
	interval  time.Duration
	debug     bool
	ctx       context.Context
	cancel    context.CancelFunc
	wg        sync.WaitGroup

	// Dry-run transaction logging
	txCounter   uint64
	txCounterMu sync.Mutex
}

// NewLogger creates a new statistics logger
func NewLogger(collector *Collector, interval time.Duration, debug bool) *Logger {
	ctx, cancel := context.WithCancel(context.Background())

	return &Logger{
		collector: collector,
		interval:  interval,
		debug:     debug,
		ctx:       ctx,
		cancel:    cancel,
	}
}

// Start begins periodic statistics logging
func (l *Logger) Start() {
	l.wg.Add(1)
	go l.logLoop()
}

// Stop gracefully shuts down the logger
func (l *Logger) Stop() {
	l.cancel()
	l.wg.Wait()
}

// LogTransaction logs individual transactions in dry-run mode
func (l *Logger) LogTransaction(tx *types.LoadTx) {
	if !l.debug {
		return
	}

	l.txCounterMu.Lock()
	l.txCounter++
	counter := l.txCounter
	l.txCounterMu.Unlock()

	// Calculate shard ID for display (assuming 4 shards for logging purposes)
	shardID := tx.ShardID(4)

	// Use JSONRPCPayload for logging since that's the actual data being sent
	log.Printf("[DEBUG TX #%d] Scenario: %s | To: %s | Shard: %d | Data: %s",
		counter, tx.Scenario.Name, tx.Scenario.Receiver.Hex(), shardID, formatTxData(tx.JSONRPCPayload))
}

// logLoop runs the periodic statistics logging
func (l *Logger) logLoop() {
	defer l.wg.Done()

	ticker := time.NewTicker(l.interval)
	defer ticker.Stop()

	for {
		select {
		case <-l.ctx.Done():
			return
		case <-ticker.C:
			l.logCurrentStats()
		}
	}
}

// logCurrentStats logs the current statistics
func (l *Logger) logCurrentStats() {
	stats := l.collector.GetStats()

	// Use a clean format that doesn't interfere with other logging
	fmt.Print(stats.FormatStats())
	fmt.Println("=============================")
}

// LogFinalStats logs comprehensive final statistics
func (l *Logger) LogFinalStats() {
	stats := l.collector.GetStats()

	fmt.Println("\n" + "=============================")
	fmt.Println("FINAL LOAD TEST RESULTS")
	fmt.Println("=============================")
	fmt.Print(stats.FormatStats())

	// Additional final statistics
	duration := time.Since(stats.StartTime)
	if duration.Seconds() > 0 {
		fmt.Printf("\nOverall Performance Summary:\n")
		fmt.Printf("  Total Runtime: %v\n", duration.Round(time.Second))
		fmt.Printf("  Total Transactions: %d\n", stats.TotalTxs)
		fmt.Printf("  Average TPS: %.2f\n", float64(stats.TotalTxs)/duration.Seconds())

		// Calculate total transactions per scenario
		scenarioTotals := make(map[string]uint64)
		for scenario, endpoints := range stats.TxCounts {
			total := uint64(0)
			for _, count := range endpoints {
				total += count
			}
			scenarioTotals[scenario] = total
		}

		fmt.Printf("\nScenario Distribution:\n")
		for scenario, total := range scenarioTotals {
			percentage := float64(total) / float64(stats.TotalTxs) * 100
			fmt.Printf("  %s: %d (%.1f%%)\n", scenario, total, percentage)
		}
	}

	fmt.Println("==============================")
}

// formatTxData formats transaction data for readable logging
func formatTxData(data []byte) string {
	if len(data) == 0 {
		return "empty"
	}

	// Show first 20 bytes in hex, truncate if longer
	if len(data) <= 20 {
		return fmt.Sprintf("0x%x", data)
	}

	return fmt.Sprintf("0x%x... (%d bytes)", data[:20], len(data))
}
