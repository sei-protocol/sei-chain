package consumer

import (
	"context"
	"time"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/metric"
)

// consumerMeterName is the OpenTelemetry meter for the Kafka -> backend offload
// consumer. It exports through the same Prometheus pipeline as the sinks, so
// consumer throughput and lag sit alongside the backend cost counters.
const consumerMeterName = "seidb_offload_consumer"

// consumerMetrics answers the two questions a cost benchmark needs beyond the
// backend's own counters: how fast is the consumer draining Kafka, and is it
// keeping up. Throughput plus lag tell you whether the backend (not the
// consumer) is the bottleneck. One consumer runs per process, so no attributes
// are needed — Prometheus job/instance labels separate backends.
type consumerMetrics struct {
	recordsProcessed metric.Int64Counter
	sinkWriteLatency metric.Float64Histogram
	kafkaLag         metric.Int64Gauge
}

func newConsumerMetrics() *consumerMetrics {
	meter := otel.Meter(consumerMeterName)
	recordsProcessed, _ := meter.Int64Counter(
		"consumer_records_processed_total",
		metric.WithDescription("Changelog records written to the backend and committed to Kafka"),
		metric.WithUnit("{record}"),
	)
	sinkWriteLatency, _ := meter.Float64Histogram(
		"consumer_sink_write_latency_seconds",
		metric.WithDescription("Latency of a batch sink write including retries; the _count series is the batch count"),
		metric.WithUnit("s"),
		metric.WithExplicitBucketBoundaries(
			0.001, 0.0025, 0.005, 0.01, 0.025, 0.05, 0.1, 0.25, 0.5, 1, 2.5, 5, 10,
		),
	)
	kafkaLag, _ := meter.Int64Gauge(
		"consumer_kafka_lag",
		metric.WithDescription("Messages between the last processed offset and the partition high watermark (max across the batch)"),
		metric.WithUnit("{message}"),
	)
	return &consumerMetrics{
		recordsProcessed: recordsProcessed,
		sinkWriteLatency: sinkWriteLatency,
		kafkaLag:         kafkaLag,
	}
}

// recordBatch reports one successfully written-and-committed batch: the number
// of records drained, how long the sink write took, and how far behind Kafka
// the batch left the consumer.
func (m *consumerMetrics) recordBatch(ctx context.Context, records int64, lag int64, writeLatency time.Duration) {
	if m == nil {
		return
	}
	if records > 0 {
		m.recordsProcessed.Add(ctx, records)
	}
	m.sinkWriteLatency.Record(ctx, writeLatency.Seconds())
	if lag >= 0 {
		m.kafkaLag.Record(ctx, lag)
	}
}
