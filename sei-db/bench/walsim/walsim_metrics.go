package walsim

import (
	"context"

	"github.com/sei-protocol/sei-chain/sei-db/common/metrics"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/metric"
)

const walsimMeterName = "walsim"

// WalsimMetrics holds OpenTelemetry metrics for the walsim benchmark.
type WalsimMetrics struct {
	ctx context.Context

	recordsWrittenTotal metric.Int64Counter
	bytesWrittenTotal   metric.Int64Counter
	flushCallsTotal     metric.Int64Counter
	pruneRequestsTotal  metric.Int64Counter

	highestIndex    metric.Int64Gauge
	lowestIndex     metric.Int64Gauge
	recordSizeBytes metric.Int64Gauge

	mainThreadPhase *metrics.PhaseTimer
}

// NewWalsimMetrics creates metrics for the walsim benchmark using the global OTel MeterProvider. The
// caller must configure the MeterProvider before calling this.
func NewWalsimMetrics(ctx context.Context, config *WalsimConfig) *WalsimMetrics {
	meter := otel.Meter(walsimMeterName)

	recordsWrittenTotal, _ := meter.Int64Counter(
		"walsim_records_written_total",
		metric.WithDescription("Total number of records appended to the WAL"),
		metric.WithUnit("{count}"),
	)
	bytesWrittenTotal, _ := meter.Int64Counter(
		"walsim_bytes_written_total",
		metric.WithDescription("Total bytes of record payload data appended to the WAL"),
		metric.WithUnit("By"),
	)
	flushCallsTotal, _ := meter.Int64Counter(
		"walsim_flush_calls_total",
		metric.WithDescription("Total number of Flush calls"),
		metric.WithUnit("{count}"),
	)
	pruneRequestsTotal, _ := meter.Int64Counter(
		"walsim_prune_requests_total",
		metric.WithDescription("Total number of prune requests issued by the benchmark (before any legacy coalescing)"),
		metric.WithUnit("{count}"),
	)

	highestIndex, _ := meter.Int64Gauge(
		"walsim_highest_index",
		metric.WithDescription("Highest record index currently stored in the WAL"),
		metric.WithUnit("{index}"),
	)
	lowestIndex, _ := meter.Int64Gauge(
		"walsim_lowest_index",
		metric.WithDescription("Lowest record index the benchmark has requested be retained"),
		metric.WithUnit("{index}"),
	)
	recordSizeBytes, _ := meter.Int64Gauge(
		"walsim_record_size_bytes",
		metric.WithDescription("Size in bytes of a single record (constant for a given config)"),
		metric.WithUnit("By"),
	)

	mainThreadPhase := metrics.NewPhaseTimer(meter, "walsim_main_thread")

	m := &WalsimMetrics{
		ctx:                 ctx,
		recordsWrittenTotal: recordsWrittenTotal,
		bytesWrittenTotal:   bytesWrittenTotal,
		flushCallsTotal:     flushCallsTotal,
		pruneRequestsTotal:  pruneRequestsTotal,
		highestIndex:        highestIndex,
		lowestIndex:         lowestIndex,
		recordSizeBytes:     recordSizeBytes,
		mainThreadPhase:     mainThreadPhase,
	}

	m.recordRecordSize(config)
	return m
}

func (m *WalsimMetrics) recordRecordSize(config *WalsimConfig) {
	if m == nil || m.recordSizeBytes == nil {
		return
	}
	m.recordSizeBytes.Record(context.Background(), int64(config.RecordSizeBytes)) //nolint:gosec // record size is bounded by config
}

// ReportRecordWritten records a single appended record of the given byte count.
func (m *WalsimMetrics) ReportRecordWritten(byteCount int64) {
	if m == nil {
		return
	}
	ctx := context.Background()
	if m.recordsWrittenTotal != nil {
		m.recordsWrittenTotal.Add(ctx, 1)
	}
	if m.bytesWrittenTotal != nil {
		m.bytesWrittenTotal.Add(ctx, byteCount)
	}
}

// ReportFlush records a Flush call.
func (m *WalsimMetrics) ReportFlush() {
	if m == nil || m.flushCallsTotal == nil {
		return
	}
	m.flushCallsTotal.Add(context.Background(), 1)
}

// ReportPruneRequest records a prune request issued by the benchmark.
func (m *WalsimMetrics) ReportPruneRequest() {
	if m == nil || m.pruneRequestsTotal == nil {
		return
	}
	m.pruneRequestsTotal.Add(context.Background(), 1)
}

// RecordHighestIndex records the highest stored record index as a gauge.
func (m *WalsimMetrics) RecordHighestIndex(index uint64) {
	if m == nil || m.highestIndex == nil {
		return
	}
	m.highestIndex.Record(context.Background(), int64(index)) //nolint:gosec // index fits in int64 for any realistic run
}

// RecordLowestIndex records the lowest retained record index as a gauge.
func (m *WalsimMetrics) RecordLowestIndex(index uint64) {
	if m == nil || m.lowestIndex == nil {
		return
	}
	m.lowestIndex.Record(context.Background(), int64(index)) //nolint:gosec // index fits in int64 for any realistic run
}

// SetMainThreadPhase records a transition of the main loop into the named phase.
func (m *WalsimMetrics) SetMainThreadPhase(phase string) {
	if m == nil || m.mainThreadPhase == nil {
		return
	}
	m.mainThreadPhase.SetPhase(phase)
}
