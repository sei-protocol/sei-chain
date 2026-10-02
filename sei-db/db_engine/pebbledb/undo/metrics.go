package undo

import (
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/metric"

	seidbmetrics "github.com/sei-protocol/sei-chain/sei-db/common/metrics"
)

// readLatencyBuckets resolves point reads from a microsecond up, where one bucket probe and a
// dozen differ.
var readLatencyBuckets = []float64{
	1e-6, 2.5e-6, 5e-6, 1e-5, 2.5e-5, 5e-5, 1e-4, 2.5e-4, 5e-4,
	1e-3, 2.5e-3, 5e-3, 1e-2, 2.5e-2, 5e-2, 0.1, 0.25, 1,
}

var (
	meter = otel.Meter("seidb_pebble_undo")

	success = metric.WithAttributes(attribute.Bool("success", true))
	failure = metric.WithAttributes(attribute.Bool("success", false))

	// otelMetrics holds the undo log's operation-level instruments. Pebble-internal stats are
	// reported by pebbledb.NewPebbleMetrics, as for the MVCC store.
	otelMetrics = struct {
		getLatency     metric.Float64Histogram
		getProbes      metric.Int64Histogram
		applyLatency   metric.Float64Histogram
		applyRecords   metric.Int64Histogram
		exciseLatency  metric.Float64Histogram
		bucketsExcised metric.Int64Counter
	}{
		getLatency: must(meter.Float64Histogram(
			"pebble_undo_get_latency",
			metric.WithDescription("Time taken to read a key at a height"),
			metric.WithUnit("s"),
			metric.WithExplicitBucketBoundaries(readLatencyBuckets...),
		)),
		getProbes: must(meter.Int64Histogram(
			"pebble_undo_get_probes",
			metric.WithDescription("Buckets probed by one read"),
			metric.WithUnit("{bucket}"),
			metric.WithExplicitBucketBoundaries(0, 1, 2, 3, 4, 6, 8, 12, 16, 24, 32),
		)),
		applyLatency: must(meter.Float64Histogram(
			"pebble_undo_apply_latency",
			metric.WithDescription("Time taken to write one block's undo records"),
			metric.WithUnit("s"),
			metric.WithExplicitBucketBoundaries(seidbmetrics.LatencyBuckets...),
		)),
		applyRecords: must(meter.Int64Histogram(
			"pebble_undo_apply_records",
			metric.WithDescription("Undo records written for one block"),
			metric.WithUnit("{record}"),
			metric.WithExplicitBucketBoundaries(seidbmetrics.CountBuckets...),
		)),
		exciseLatency: must(meter.Float64Histogram(
			"pebble_undo_excise_latency",
			metric.WithDescription("Time taken to excise expired buckets"),
			metric.WithUnit("s"),
			metric.WithExplicitBucketBoundaries(seidbmetrics.LatencyBuckets...),
		)),
		bucketsExcised: must(meter.Int64Counter(
			"pebble_undo_buckets_excised",
			metric.WithDescription("Buckets removed by Excise"),
			metric.WithUnit("{bucket}"),
		)),
	}
)

// must panics if err is non-nil, otherwise returns v.
func must[V any](v V, err error) V {
	if err != nil {
		panic(err)
	}
	return v
}
