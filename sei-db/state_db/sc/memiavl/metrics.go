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
		SnapshotRewriteLatency  metric.Float64Histogram
		SnapshotCreationCount   metric.Int64Counter
		SnapshotPruneLatency    metric.Float64Histogram
		CatchupReplayLatency    metric.Float64Histogram
		CatchupReplayNumBlocks  metric.Int64Counter
		CurrentSnapshotHeight   metric.Int64Gauge
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
			metric.WithDescription("Total time taken to create memiavl snapshot + replay"),
			metric.WithUnit("s"),
		)),
		SnapshotRewriteLatency: must(meter.Float64Histogram(
			"memiavl_snapshot_rewrite_latency",
			metric.WithDescription("Time taken to write to the new memiavl snapshot"),
			metric.WithUnit("s"),
		)),
		SnapshotCreationCount: must(meter.Int64Counter(
			"memiavl_snapshot_creation_count",
			metric.WithDescription("Total num of times memiavl snapshot creation happens"),
			metric.WithUnit("By"))),
		SnapshotPruneLatency: must(meter.Float64Histogram(
			"memiavl_snapshot_prune_latency",
			metric.WithDescription("Time taken to prune memiavl snapshot"),
			metric.WithUnit("s"),
		)),
		CatchupReplayLatency: must(meter.Float64Histogram(
			"memiavl_snapshot_catchup_replay_latency",
			metric.WithDescription("Time taken to catchup and replay after snapshot rewrite"),
			metric.WithUnit("s"),
		)),
		CatchupReplayNumBlocks: must(meter.Int64Counter(
			"memiavl_snapshot_catchup_replay_num_blocks",
			metric.WithDescription("Num of blocks memIAVL has replayed after snapshot creation"),
			metric.WithUnit("{count}"),
		)),
		CurrentSnapshotHeight: must(meter.Int64Gauge(
			"memiavl_current_snapshot_height",
			metric.WithDescription("Current snapshot height"),
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
