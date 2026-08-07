package seiwal

import (
	"context"
	"time"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/metric"

	commonmetrics "github.com/sei-protocol/sei-chain/sei-db/common/metrics"
)

// The name of the OpenTelemetry meter for WAL metrics.
const walMeterName = "seidb_seiwal"

// Instruments are shared process-wide (created once); individual WAL instances are distinguished by the
// "wal" attribute attached at each recording (see walMetrics), mirroring LittDB's per-table labeling. This
// keeps metrics from multiple instances in one process from clobbering each other — so long as each instance
// has its own name. Instances that must share one disable their metrics instead.
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

// walMetrics records one WAL instance's measurements. An instance whose metrics are disabled records nothing
// at all.
//
// Every instrument above is labeled by the instance name alone, and seiwal_queue_depth is a gauge, so two
// live instances sharing a name overwrite each other's samples. Disabling metrics is how such an instance
// opts out; see Config.DisableMetrics.
type walMetrics struct {
	// Whether measurements are recorded at all.
	enabled bool

	// Tags an observation with the instance name, so metrics from distinct instances stay distinguishable.
	nameAttrs metric.MeasurementOption

	// Tags a queue-depth observation with the instance name and the internal channel being measured.
	queueAttrs metric.MeasurementOption
}

// newWALMetrics returns the recorder for the WAL instance described by config, whose queue-depth samples are
// tagged with the internal channel named by queue ("writer" or "serializer").
//
// The queue is fixed per instance rather than passed per observation: within one generic WAL the inner byte
// WAL and the outer serializing WAL share a name, and this label is what keeps their samples apart.
func newWALMetrics(config *Config, queue string) walMetrics {
	return walMetrics{
		enabled:   !config.DisableMetrics,
		nameAttrs: metric.WithAttributeSet(attribute.NewSet(attribute.String("wal", config.Name))),
		queueAttrs: metric.WithAttributeSet(attribute.NewSet(
			attribute.String("wal", config.Name), attribute.String("queue", queue))),
	}
}

// recordAppend records one record of recordBytes framed bytes appended to the WAL.
func (m walMetrics) recordAppend(ctx context.Context, recordBytes int) {
	if !m.enabled {
		return
	}
	walBytesWritten.Add(ctx, int64(recordBytes), m.nameAttrs)
	walRecordsWritten.Add(ctx, 1, m.nameAttrs)
}

// recordFileSealed records one WAL file sealed on rotation.
func (m walMetrics) recordFileSealed(ctx context.Context) {
	if !m.enabled {
		return
	}
	walFilesSealed.Add(ctx, 1, m.nameAttrs)
}

// recordFilePruned records one sealed WAL file removed by pruning.
func (m walMetrics) recordFilePruned(ctx context.Context) {
	if !m.enabled {
		return
	}
	walFilesPruned.Add(ctx, 1, m.nameAttrs)
}

// recordSerializeError records one serialization failure.
func (m walMetrics) recordSerializeError(ctx context.Context) {
	if !m.enabled {
		return
	}
	walSerializeErrors.Add(ctx, 1, m.nameAttrs)
}

// recordSerialized records a successful serialization that took elapsed and produced payloadBytes bytes.
func (m walMetrics) recordSerialized(ctx context.Context, elapsed time.Duration, payloadBytes int) {
	if !m.enabled {
		return
	}
	walSerializeDuration.Record(ctx, elapsed.Seconds(), m.nameAttrs)
	walSerializedBytes.Add(ctx, int64(payloadBytes), m.nameAttrs)
}

// recordQueueDepth records the current buffered depth of this instance's internal channel.
func (m walMetrics) recordQueueDepth(ctx context.Context, depth int) {
	if !m.enabled {
		return
	}
	walQueueDepth.Record(ctx, int64(depth), m.queueAttrs)
}

func must[V any](v V, err error) V {
	if err != nil {
		panic(err)
	}
	return v
}
