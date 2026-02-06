package benchmark

import (
	"context"
	"sync"
	"time"

	"github.com/tendermint/tendermint/libs/log"
)

// Logger tracks benchmark metrics and periodically logs statistics.
type Logger struct {
	mx             sync.Mutex
	txCount        int64         // Total transactions processed
	blockCount     int64         // Number of times Increment was called (number of blocks)
	latestHeight   int64         // Highest height seen in the window
	maxBlockTime   time.Duration // Maximum time difference between consecutive blocks
	totalBlockTime time.Duration // Sum of all block time differences in the window
	blockTimeCount int64         // Number of block time differences calculated
	prevBlockTime  time.Time     // Previous block time for calculating differences
	lastFlushTime  time.Time     // When we last flushed (for TPS calculation)
	// Commit time tracking
	maxCommitTime   time.Duration // Maximum commit time in the window
	totalCommitTime time.Duration // Sum of all commit times in the window
	commitCount     int64         // Number of commits in the window
	// Block processing time tracking (ProcessProposal start to FinalizeBlock end)
	blockProcessStartTime time.Time     // Start time of current block processing
	maxBlockProcessTime   time.Duration // Maximum block processing time in the window
	totalBlockProcessTime time.Duration // Sum of all block processing times in the window
	blockProcessCount     int64         // Number of block processing times recorded
	logger                log.Logger
}

// NewLogger creates a new benchmark logger.
func NewLogger(logger log.Logger) *Logger {
	return &Logger{
		logger: logger,
	}
}

// Increment records transaction count and block timing information.
func (l *Logger) Increment(count int64, blocktime time.Time, height int64) {
	l.mx.Lock()
	defer l.mx.Unlock()

	// Initialize lastFlushTime on first increment (when blocks actually start processing)
	if l.lastFlushTime.IsZero() {
		l.lastFlushTime = time.Now()
	}

	l.txCount += count
	l.blockCount++
	if height > l.latestHeight {
		l.latestHeight = height
	}

	// Calculate time difference between consecutive blocks
	if !l.prevBlockTime.IsZero() {
		blockTimeDiff := blocktime.Sub(l.prevBlockTime)
		if blockTimeDiff > l.maxBlockTime {
			l.maxBlockTime = blockTimeDiff
		}
		l.totalBlockTime += blockTimeDiff
		l.blockTimeCount++
	}
	l.prevBlockTime = blocktime
}

// RecordCommitTime records the duration of a commit operation.
func (l *Logger) RecordCommitTime(duration time.Duration) {
	l.mx.Lock()
	defer l.mx.Unlock()

	if duration > l.maxCommitTime {
		l.maxCommitTime = duration
	}
	l.totalCommitTime += duration
	l.commitCount++
}

// StartBlockProcessing marks the start of block processing (at ProcessProposal).
func (l *Logger) StartBlockProcessing() {
	l.mx.Lock()
	defer l.mx.Unlock()
	l.blockProcessStartTime = time.Now()
}

// EndBlockProcessing marks the end of block processing (at FinalizeBlock end) and records the duration.
func (l *Logger) EndBlockProcessing() {
	l.mx.Lock()
	defer l.mx.Unlock()

	if l.blockProcessStartTime.IsZero() {
		return
	}

	duration := time.Since(l.blockProcessStartTime)
	if duration > l.maxBlockProcessTime {
		l.maxBlockProcessTime = duration
	}
	l.totalBlockProcessTime += duration
	l.blockProcessCount++
	l.blockProcessStartTime = time.Time{} // Reset for next block
}

// calculateTPS computes transactions per second based on transaction count and duration.
func calculateTPS(txCount int64, duration time.Duration) float64 {
	if duration <= 0 {
		return 0
	}
	return float64(txCount) / duration.Seconds()
}

// calculateAvgBlockTime computes the average block time from total block time and count.
func calculateAvgBlockTime(totalBlockTime time.Duration, blockTimeCount int64) int64 {
	if blockTimeCount <= 0 {
		return 0
	}
	avgBlockTime := totalBlockTime / time.Duration(blockTimeCount)
	return avgBlockTime.Milliseconds()
}

// calculateTheoreticalTPS computes the maximum possible TPS if blocks arrived instantly.
// It divides average transactions per block by average block processing time.
func calculateTheoreticalTPS(txCount, blockCount, avgBlockProcessMs int64) float64 {
	if blockCount <= 0 || avgBlockProcessMs <= 0 {
		return 0.0
	}
	avgTxsPerBlock := float64(txCount) / float64(blockCount)
	return avgTxsPerBlock * 1000 / float64(avgBlockProcessMs)
}

// flushStats holds the statistics for a flush window.
type flushStats struct {
	txCount           int64
	blockCount        int64
	latestHeight      int64
	maxBlockTimeMs    int64
	avgBlockTimeMs    int64
	maxCommitTimeMs   int64
	avgCommitTimeMs   int64
	maxBlockProcessMs int64
	avgBlockProcessMs int64
	tps               float64
	theoreticalTps    float64 // TPS based on blockProcessAvg (if blocks arrived instantly)
}

// getAndResetStats atomically reads current stats and resets counters for next window.
func (l *Logger) getAndResetStats(now time.Time) (flushStats, time.Time) {
	l.mx.Lock()
	defer l.mx.Unlock()

	stats := flushStats{
		txCount:           l.txCount,
		blockCount:        l.blockCount,
		latestHeight:      l.latestHeight,
		maxBlockTimeMs:    l.maxBlockTime.Milliseconds(),
		maxCommitTimeMs:   l.maxCommitTime.Milliseconds(),
		maxBlockProcessMs: l.maxBlockProcessTime.Milliseconds(),
	}

	prevTime := l.lastFlushTime
	totalBlockTime := l.totalBlockTime
	blockTimeCount := l.blockTimeCount
	totalCommitTime := l.totalCommitTime
	commitCount := l.commitCount
	totalBlockProcessTime := l.totalBlockProcessTime
	blockProcessCount := l.blockProcessCount

	// Reset counters for next window (but keep prevBlockTime and blockProcessStartTime for continuity)
	l.txCount = 0
	l.blockCount = 0
	l.latestHeight = 0
	l.maxBlockTime = 0
	l.totalBlockTime = 0
	l.blockTimeCount = 0
	l.maxCommitTime = 0
	l.totalCommitTime = 0
	l.commitCount = 0
	l.maxBlockProcessTime = 0
	l.totalBlockProcessTime = 0
	l.blockProcessCount = 0
	l.lastFlushTime = now

	// Calculate TPS
	duration := now.Sub(prevTime)
	if duration > 0 && !prevTime.IsZero() {
		stats.tps = calculateTPS(stats.txCount, duration)
	}

	// Calculate average block time
	stats.avgBlockTimeMs = calculateAvgBlockTime(totalBlockTime, blockTimeCount)

	// Calculate average commit time
	if commitCount > 0 {
		stats.avgCommitTimeMs = (totalCommitTime / time.Duration(commitCount)).Milliseconds()
	}

	// Calculate average block processing time
	if blockProcessCount > 0 {
		stats.avgBlockProcessMs = (totalBlockProcessTime / time.Duration(blockProcessCount)).Milliseconds()
	}

	// Calculate theoretical TPS based on block processing time
	// This is the TPS we could achieve if blocks arrived instantly
	stats.theoreticalTps = calculateTheoreticalTPS(stats.txCount, stats.blockCount, stats.avgBlockProcessMs)

	return stats, prevTime
}

// FlushLog outputs the current statistics.
func (l *Logger) FlushLog() {
	now := time.Now()
	stats, _ := l.getAndResetStats(now)

	l.logger.Info("benchmark",
		"txs", stats.txCount,
		"blocks", stats.blockCount,
		"height", stats.latestHeight,
		"blockTimeMax", stats.maxBlockTimeMs,
		"blockTimeAvg", stats.avgBlockTimeMs,
		"commitTimeMax", stats.maxCommitTimeMs,
		"commitTimeAvg", stats.avgCommitTimeMs,
		"blockProcessMax", stats.maxBlockProcessMs,
		"blockProcessAvg", stats.avgBlockProcessMs,
		"tps", stats.tps,
		"theoreticalTps", stats.theoreticalTps,
	)
}

// Start begins the periodic logging goroutine.
func (l *Logger) Start(ctx context.Context) {
	ticker := time.NewTicker(5 * time.Second)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			l.FlushLog()
		}
	}
}
