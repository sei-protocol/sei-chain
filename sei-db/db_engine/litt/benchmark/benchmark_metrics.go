package benchmark

import (
	"context"
	"fmt"
	"sync/atomic"
	"time"

	"github.com/Layr-Labs/eigenda/common"
	"github.com/Layr-Labs/eigenda/litt/benchmark/config"
	"github.com/Layr-Labs/eigensdk-go/logging"
)

// metrics is a struct that holds various performance metrics for the benchmark. If configured, periodically
// writes a summary to the log. The intention is to expose data about the benchmark's performance even if
// prometheus is not available or configured.
type metrics struct {
	ctx    context.Context
	logger logging.Logger

	// The configuration for the benchmark.
	config *config.BenchmarkConfig

	// The time when the benchmark started.
	startTime time.Time

	// The number of bytes written since the benchmark started.
	bytesWritten atomic.Uint64

	// The number of bytes read since the benchmark started.
	bytesRead atomic.Uint64

	// The number of write operations performed since the benchmark started.
	writeCount atomic.Uint64

	// The number of read operations performed since the benchmark started.
	readCount atomic.Uint64

	// The number of flush operations performed since the benchmark started.
	flushCount atomic.Uint64

	// The amount of time spent writing data.
	nanosecondsSpentWriting atomic.Uint64

	// The amount of time spent reading data.
	nanosecondsSpentReading atomic.Uint64

	// The amount of time spent flushing data.
	nanosecondsSpentFlushing atomic.Uint64

	// Longest write duration observed.
	longestWriteDuration atomic.Uint64

	// Longest read duration observed.
	longestReadDuration atomic.Uint64

	// Longest flush duration observed.
	longestFlushDuration atomic.Uint64
}

// newMetrics initializes a new metrics object.
func newMetrics(
	ctx context.Context,
	logger logging.Logger,
	config *config.BenchmarkConfig,
) *metrics {

	m := &metrics{
		ctx:       ctx,
		logger:    logger,
		config:    config,
		startTime: time.Now(),
	}

	go m.reportGenerator()
	return m
}

// reportWrite records a write operation.
func (m *metrics) reportWrite(writeDuration time.Duration, bytesWritten uint64) {
	m.writeCount.Add(1)
	m.bytesWritten.Add(bytesWritten)
	m.nanosecondsSpentWriting.Add(uint64(writeDuration.Nanoseconds()))

	// Update the longest write duration if this one is longer.
	currentLongest := m.longestWriteDuration.Load()
	for writeDuration.Nanoseconds() > int64(currentLongest) {
		swapped := m.longestWriteDuration.CompareAndSwap(currentLongest, uint64(writeDuration.Nanoseconds()))
		if swapped {
			break
		}
		currentLongest = m.longestWriteDuration.Load()
	}
}

// reportRead records a read operation.
func (m *metrics) reportRead(readDuration time.Duration, bytesRead uint64) {
	m.readCount.Add(1)
	m.bytesRead.Add(bytesRead)
	m.nanosecondsSpentReading.Add(uint64(readDuration.Nanoseconds()))

	// Update the longest read duration if this one is longer.
	currentLongest := m.longestReadDuration.Load()
	for readDuration.Nanoseconds() > int64(currentLongest) {
		swapped := m.longestReadDuration.CompareAndSwap(currentLongest, uint64(readDuration.Nanoseconds()))
		if swapped {
			break
		}
		currentLongest = m.longestReadDuration.Load()
	}
}

// reportFlush records a flush operation.
func (m *metrics) reportFlush(flushDuration time.Duration) {
	m.flushCount.Add(1)
	m.nanosecondsSpentFlushing.Add(uint64(flushDuration.Nanoseconds()))

	// Update the longest flush duration if this one is longer.
	currentLongest := m.longestFlushDuration.Load()
	for flushDuration.Nanoseconds() > int64(currentLongest) {
		swapped := m.longestFlushDuration.CompareAndSwap(currentLongest, uint64(flushDuration.Nanoseconds()))
		if swapped {
			break
		}
		currentLongest = m.longestFlushDuration.Load()
	}
}

// reportGenerator runs in a goroutine and periodically logs the metrics to the console.
func (m *metrics) reportGenerator() {
	if m.config.MetricsLoggingPeriodSeconds <= 0 {
		return // Metrics logging is disabled.
	}

	ticker := time.NewTicker(time.Duration(m.config.MetricsLoggingPeriodSeconds * float64(time.Second)))
	defer ticker.Stop()

	for {
		select {
		case <-m.ctx.Done():
			return // Context cancelled, stop reporting.
		case <-ticker.C:
			m.logMetrics()
		}
	}
}

// logMetrics logs the current metrics to the console.
func (m *metrics) logMetrics() {

	averageWriteLatency := uint64(0)
	writeCount := m.writeCount.Load()
	if writeCount > 0 {
		averageWriteLatency =
			uint64((time.Duration(m.nanosecondsSpentWriting.Load()) / time.Duration(writeCount)).Nanoseconds())
	}

	averageReadLatency := uint64(0)
	readCount := m.readCount.Load()
	if readCount > 0 {
		averageReadLatency =
			uint64((time.Duration(m.nanosecondsSpentReading.Load()) / time.Duration(readCount)).Nanoseconds())
	}

	averageFlushLatency := uint64(0)
	flushCount := m.flushCount.Load()
	if flushCount > 0 {
		averageFlushLatency =
			uint64((time.Duration(m.nanosecondsSpentFlushing.Load()) / time.Duration(flushCount)).Nanoseconds())
	}

	elapsedTimeNanoseconds := uint64(time.Since(m.startTime).Nanoseconds())
	elapsedTimeSeconds := float64(elapsedTimeNanoseconds) / float64(time.Second)

	bytesWritten := m.bytesWritten.Load()
	writeThroughput := uint64(0)
	if elapsedTimeSeconds > 0 {
		writeThroughput = uint64(float64(bytesWritten) / elapsedTimeSeconds)
	}

	readThroughput := uint64(0)
	if elapsedTimeSeconds > 0 {
		readThroughput = uint64(float64(m.bytesRead.Load()) / elapsedTimeSeconds)
	}

	totalTime := ""
	if m.config.TimeLimitSeconds > 0 {
		totalTime = fmt.Sprintf(" / %s",
			common.PrettyPrintTime(uint64(m.config.TimeLimitSeconds*float64(time.Second))))
	}

	m.logger.Infof("Benchmark Metrics (since most recent restart):\n"+
		"    Elapsed Time:           %s%s\n\n"+
		"    Write Throughput:       %s/s\n"+
		"    Bytes Written:          %s\n"+
		"    Write Count:            %s\n"+
		"    Average Write Latency:  %s\n"+
		"    Longest Write Duration: %s\n\n"+
		"    Read Throughput:        %s/s\n"+
		"    Bytes Read:             %s\n"+
		"    Read Count:             %s\n"+
		"    Average Read Latency:   %s\n"+
		"    Longest Read Duration:  %s\n\n"+
		"    Flush Count:            %s\n"+
		"    Average Flush Latency:  %s\n"+
		"    Longest Flush Duration: %s",
		common.PrettyPrintTime(elapsedTimeNanoseconds),
		totalTime,
		common.PrettyPrintBytes(writeThroughput),
		common.PrettyPrintBytes(bytesWritten),
		common.CommaOMatic(writeCount),
		common.PrettyPrintTime(averageWriteLatency),
		common.PrettyPrintTime(m.longestWriteDuration.Load()),
		common.PrettyPrintBytes(readThroughput),
		common.PrettyPrintBytes(m.bytesRead.Load()),
		common.CommaOMatic(readCount),
		common.PrettyPrintTime(averageReadLatency),
		common.PrettyPrintTime(m.longestReadDuration.Load()),
		common.CommaOMatic(flushCount),
		common.PrettyPrintTime(averageFlushLatency),
		common.PrettyPrintTime(m.longestFlushDuration.Load()))
}
