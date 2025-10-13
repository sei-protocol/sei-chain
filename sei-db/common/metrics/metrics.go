package metrics

import (
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/metric"
)

var (
	meter = otel.Meter("seidb")

	SeiDBMetrics = struct {
		RestartLatency          metric.Float64Histogram
		SnapshotCreationLatency metric.Float64Histogram
		CommitLatency           metric.Int64Histogram
		ApplyChangesetLatency   metric.Int64Histogram
		NumOfKVPairs            metric.Int64Counter
		MemNodeTotalSize        metric.Int64Gauge
		NumOfMemNode            metric.Int64Gauge
	}{
		RestartLatency: must(meter.Float64Histogram(
			"restart_latency",
			metric.WithDescription("Time taken to restart the memiavl database"),
			metric.WithUnit("s"),
		)),
		SnapshotCreationLatency: must(meter.Float64Histogram(
			"snapshot_creation_latency",
			metric.WithDescription("Time taken to create memiavl snapshot"),
			metric.WithUnit("s"),
		)),
		CommitLatency: must(meter.Int64Histogram(
			"commit_latency",
			metric.WithDescription("Time taken to commit"),
			metric.WithUnit("ms"),
		)),
		ApplyChangesetLatency: must(meter.Int64Histogram(
			"apply_changeset_latency",
			metric.WithDescription("Time taken to apply changesets"),
			metric.WithUnit("ms"),
		)),
		NumOfKVPairs: must(meter.Int64Counter(
			"num_of_kv_pairs",
			metric.WithDescription("Num of kv pairs in apply changesets"),
			metric.WithUnit("{count}"),
		)),
		MemNodeTotalSize: must(meter.Int64Gauge(
			"mem_node_total_size",
			metric.WithDescription("Total size of memnodes"),
			metric.WithUnit("By"),
		)),
		NumOfMemNode: must(meter.Int64Gauge(
			"mem_node_count",
			metric.WithDescription("Total number of mem nodes"),
			metric.WithUnit("{count}"),
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
