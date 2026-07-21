package bigtable

import (
	"context"
	"time"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/metric"
)

// bigtableMeterName is the OpenTelemetry meter used for Bigtable-backed
// historical-state access. It exports through the same process-wide
// MeterProvider (Prometheus exporter) as the rest of SeiDB.
const bigtableMeterName = "seidb_bigtable"

// bigtableMetrics measures the cost of Bigtable-backed historical state so it
// is easy to see how read and write volume scales with historical query and
// ingestion load. The three cost drivers Bigtable bills on — request count,
// rows touched, and bytes transferred — are each captured directly:
//
//   - request count: the *_latency_seconds histogram's _count series (free);
//   - rows touched:  bigtable_rows_read_total / bigtable_rows_mutated_total;
//   - bytes moved:   bigtable_bytes_read_total / bigtable_bytes_written_total.
//
// All series carry only a single low-cardinality "table" attribute, so the
// storage footprint stays flat as request volume grows.
type bigtableMetrics struct {
	readLatency  metric.Float64Histogram
	rowsRead     metric.Int64Counter
	bytesRead    metric.Int64Counter
	writeLatency metric.Float64Histogram
	rowsMutated  metric.Int64Counter
	bytesWritten metric.Int64Counter
}

func newBigtableMetrics() *bigtableMetrics {
	meter := otel.Meter(bigtableMeterName)
	// Buckets span single-digit-ms cache-warm reads to multi-second slow RPCs.
	latencyBuckets := metric.WithExplicitBucketBoundaries(
		0.001, 0.0025, 0.005, 0.01, 0.025, 0.05, 0.1, 0.25, 0.5, 1, 2.5, 5, 10,
	)
	readLatency, _ := meter.Float64Histogram(
		"bigtable_read_latency_seconds",
		metric.WithDescription("Latency of Bigtable ReadRows RPCs for historical state; the _count series doubles as the read request count"),
		metric.WithUnit("s"),
		latencyBuckets,
	)
	rowsRead, _ := meter.Int64Counter(
		"bigtable_rows_read_total",
		metric.WithDescription("Rows returned by Bigtable ReadRows RPCs for historical state"),
		metric.WithUnit("{row}"),
	)
	bytesRead, _ := meter.Int64Counter(
		"bigtable_bytes_read_total",
		metric.WithDescription("Bytes (row keys + cell values) returned by Bigtable reads for historical state"),
		metric.WithUnit("By"),
	)
	writeLatency, _ := meter.Float64Histogram(
		"bigtable_mutate_latency_seconds",
		metric.WithDescription("Latency of Bigtable MutateRows RPCs for historical-state ingestion; the _count series doubles as the mutate request count"),
		metric.WithUnit("s"),
		latencyBuckets,
	)
	rowsMutated, _ := meter.Int64Counter(
		"bigtable_rows_mutated_total",
		metric.WithDescription("Rows written by Bigtable MutateRows RPCs for historical-state ingestion"),
		metric.WithUnit("{row}"),
	)
	bytesWritten, _ := meter.Int64Counter(
		"bigtable_bytes_written_total",
		metric.WithDescription("Bytes (row keys + cell values) written by Bigtable mutations for historical state"),
		metric.WithUnit("By"),
	)
	return &bigtableMetrics{
		readLatency:  readLatency,
		rowsRead:     rowsRead,
		bytesRead:    bytesRead,
		writeLatency: writeLatency,
		rowsMutated:  rowsMutated,
		bytesWritten: bytesWritten,
	}
}

// recordRead reports one ReadRows RPC: its latency plus the rows and bytes it
// returned. It records even when the RPC fails, since a failed read still costs.
func (m *bigtableMetrics) recordRead(ctx context.Context, table string, latency time.Duration, rows, bytes int64) {
	if m == nil {
		return
	}
	attrs := metric.WithAttributes(attribute.String("table", table))
	m.readLatency.Record(ctx, latency.Seconds(), attrs)
	if rows > 0 {
		m.rowsRead.Add(ctx, rows, attrs)
	}
	if bytes > 0 {
		m.bytesRead.Add(ctx, bytes, attrs)
	}
}

// recordWrite reports one MutateRows RPC: its latency plus the rows and bytes
// it wrote (attempted). It records even on failure, since the request still costs.
func (m *bigtableMetrics) recordWrite(ctx context.Context, table string, latency time.Duration, rows, bytes int64) {
	if m == nil {
		return
	}
	attrs := metric.WithAttributes(attribute.String("table", table))
	m.writeLatency.Record(ctx, latency.Seconds(), attrs)
	if rows > 0 {
		m.rowsMutated.Add(ctx, rows, attrs)
	}
	if bytes > 0 {
		m.bytesWritten.Add(ctx, bytes, attrs)
	}
}

// rowSize estimates the transferred size of a read row: the row key
// plus each returned cell's qualifier and value.
func rowSize(row Row) int64 {
	size := int64(len(row.Key))
	for _, cell := range row.Cells {
		size += int64(len(cell.Qualifier)) + int64(len(cell.Value))
	}
	return size
}

// mutationSize estimates the written size of a mutation row: the row
// key plus each set cell's qualifier and value.
func mutationSize(row RowMutation) int64 {
	size := int64(len(row.RowKey))
	for _, cell := range row.SetCells {
		size += int64(len(cell.Qualifier)) + int64(len(cell.Value))
	}
	return size
}
