package seiwal

import (
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/metric"

	commonmetrics "github.com/sei-protocol/sei-chain/sei-db/common/metrics"
)

// The name of the OpenTelemetry meter for WAL metrics.
const walMeterName = "seidb_seiwal"

// Instruments are shared process-wide (created once); individual WAL instances are distinguished by the
// "wal" attribute attached at each recording (see walNameAttr), mirroring LittDB's per-table labeling. This
// keeps metrics from multiple instances in one process from clobbering each other.
var (
	walMeter = otel.Meter(walMeterName)

	// The number of records appended to the WAL.
	walRecordsWritten = must(walMeter.Int64Counter(
		"seiwal_records_written",
		metric.WithDescription("Number of records appended to the WAL"),
		metric.WithUnit("{count}"),
	))

	// The number of record bytes appended to the WAL (including framing).
	walBytesWritten = must(walMeter.Int64Counter(
		"seiwal_bytes_written",
		metric.WithDescription("Number of bytes written to the WAL"),
		metric.WithUnit("By"),
	))

	// The number of WAL files sealed (rotated) after reaching the target size.
	walFilesSealed = must(walMeter.Int64Counter(
		"seiwal_files_sealed",
		metric.WithDescription("Number of WAL files sealed on rotation"),
		metric.WithUnit("{count}"),
	))

	// The number of sealed WAL files deleted by pruning.
	walFilesPruned = must(walMeter.Int64Counter(
		"seiwal_files_pruned",
		metric.WithDescription("Number of WAL files removed by pruning"),
		metric.WithUnit("{count}"),
	))

	// The time spent serializing a payload in the generic serializing WAL.
	walSerializeDuration = must(walMeter.Float64Histogram(
		"seiwal_serialize_duration_seconds",
		metric.WithDescription("Time spent serializing a payload in the generic WAL"),
		metric.WithUnit("s"),
		metric.WithExplicitBucketBoundaries(commonmetrics.LatencyBuckets...),
	))

	// The number of payload bytes produced by serialization in the generic serializing WAL.
	walSerializedBytes = must(walMeter.Int64Counter(
		"seiwal_serialized_bytes",
		metric.WithDescription("Number of payload bytes produced by serialization in the generic WAL"),
		metric.WithUnit("By"),
	))

	// The number of serialization failures in the generic serializing WAL.
	walSerializeErrors = must(walMeter.Int64Counter(
		"seiwal_serialize_errors",
		metric.WithDescription("Number of serialization failures in the generic WAL"),
		metric.WithUnit("{count}"),
	))

	// The buffered depth of a WAL's internal channel, sampled periodically.
	walQueueDepth = must(walMeter.Int64Gauge(
		"seiwal_queue_depth",
		metric.WithDescription("Buffered depth of a WAL internal channel, sampled periodically"),
		metric.WithUnit("{count}"),
	))
)

// walNameAttr returns the measurement option that tags an observation with a WAL instance's name, so metrics
// from distinct instances in the same process remain distinguishable.
func walNameAttr(name string) metric.MeasurementOption {
	return metric.WithAttributeSet(attribute.NewSet(attribute.String("wal", name)))
}

// queueDepthAttrs tags a queue-depth observation with the WAL instance name and which internal channel
// ("writer" or "serializer") is being measured.
func queueDepthAttrs(name string, queue string) metric.MeasurementOption {
	return metric.WithAttributeSet(attribute.NewSet(attribute.String("wal", name), attribute.String("queue", queue)))
}

func must[V any](v V, err error) V {
	if err != nil {
		panic(err)
	}
	return v
}
