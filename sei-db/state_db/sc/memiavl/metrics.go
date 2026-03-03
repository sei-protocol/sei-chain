package memiavl

import (
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/metric"
)

var (
	meter = otel.Meter("seidb_memiavl")

	otelMetrics = struct {
		RestartLatency             metric.Float64Histogram
		SnapshotRewriteLatency     metric.Float64Histogram
		NumSnapshotRewriteAttempts metric.Int64Counter
		SnapshotPruneLatency       metric.Float64Histogram
		NumSnapshotPruneAttempts   metric.Int64Counter
		CatchupAfterRewriteLatency metric.Float64Histogram
		CatchupBeforeReloadLatency metric.Float64Histogram
		CatchupReplayNumBlocks     metric.Int64Counter
		CurrentSnapshotHeight      metric.Int64Gauge
		CommitLatency              metric.Float64Histogram
		ApplyChangesetLatency      metric.Float64Histogram
		NumOfKVPairs               metric.Int64Counter
		MemNodeTotalSize           metric.Int64Gauge
		NumOfMemNode               metric.Int64Gauge
	}{
		RestartLatency: must(meter.Float64Histogram(
			"memiavl_restart_latency",
			metric.WithDescription("Time taken to restart the memiavl database"),
			metric.WithUnit("s"),
		)),
		SnapshotRewriteLatency: must(meter.Float64Histogram(
			"memiavl_snapshot_rewrite_latency",
			metric.WithDescription("Time taken to write to the new memiavl snapshot"),
			metric.WithUnit("s"),
		)),
		NumSnapshotRewriteAttempts: must(meter.Int64Counter(
			"memiavl_num_snapshot_rewrite_attempts",
			metric.WithDescription("Total num of memiavl snapshot rewrite attempts"),
			metric.WithUnit("{count}"))),
		SnapshotPruneLatency: must(meter.Float64Histogram(
			"memiavl_snapshot_prune_latency",
			metric.WithDescription("Time taken to prune memiavl snapshot"),
			metric.WithUnit("s"),
		)),
		NumSnapshotPruneAttempts: must(meter.Int64Counter(
			"memiavl_num_snapshot_prune_attempts",
			metric.WithDescription("Total number of snapshot prune attempts"),
			metric.WithUnit("{count}"),
		)),
		CatchupAfterRewriteLatency: must(meter.Float64Histogram(
			"memiavl_snapshot_catchup_after_rewrite_latency",
			metric.WithDescription("Time taken to catchup and replay after snapshot rewrite"),
			metric.WithUnit("s"),
		)),
		CatchupBeforeReloadLatency: must(meter.Float64Histogram(
			"memiavl_snapshot_catchup_before_reload_latency",
			metric.WithDescription("Time taken to catchup and replay before switch to new snapshot"),
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
