package memiavl

import (
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/metric"
)

var (
	meter = otel.Meter("seidb_memiavl")

	otelMetrics = struct {
		RestartLatency          metric.Float64Histogram
		SnapshotCreationLatency metric.Float64Histogram
		CommitLatency           metric.Float64Histogram
		ApplyChangesetLatency   metric.Float64Histogram
		NumOfKVPairs            metric.Int64Counter
		MemNodeTotalSize        metric.Int64Gauge
		NumOfMemNode            metric.Int64Gauge
	}{
		RestartLatency: must(meter.Float64Histogram(
			"memiavl_restart_latency",
			metric.WithDescription("Time taken to restart the memiavl database"),
			metric.WithUnit("s"),
		)),
		SnapshotCreationLatency: must(meter.Float64Histogram(
			"memiavl_snapshot_creation_latency",
			metric.WithDescription("Time taken to create memiavl snapshot"),
			metric.WithUnit("s"),
		)),
		CommitLatency: must(meter.Float64Histogram(
			"memiavl_commit_latency",
			metric.WithDescription("Time taken to commit"),
			metric.WithUnit("s"),
		)),
		ApplyChangesetLatency: must(meter.Float64Histogram(
			"memiavl_apply_changeset_latency",
			metric.WithDescription("Time taken to apply changesets"),
			metric.WithUnit("s"),
		)),
		NumOfKVPairs: must(meter.Int64Counter(
			"memiavl_num_of_kv_pairs",
			metric.WithDescription("Num of kv pairs in apply changesets"),
			metric.WithUnit("{count}"),
		)),
		MemNodeTotalSize: must(meter.Int64Gauge(
			"memiavl_mem_node_total_size",
			metric.WithDescription("Total size of memnodes"),
			metric.WithUnit("By"),
		)),
		NumOfMemNode: must(meter.Int64Gauge(
			"memiavl_mem_node_count",
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
