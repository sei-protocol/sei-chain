package flatkv

import (
	"context"
	"time"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/metric"
)

var (
	flatkvMeter = otel.Meter(flatkvMeterName)

	otelMetrics = struct {
		OpenLatency               metric.Float64Histogram
		ApplyChangesetsLatency    metric.Float64Histogram
		CommitLatency             metric.Float64Histogram
		CommitBatchLatency        metric.Float64Histogram
		BatchReadOldValuesLatency metric.Float64Histogram
		NumKVPairs                metric.Int64Counter
		PendingWrites             metric.Int64Gauge
		CurrentVersion            metric.Int64Gauge
		CatchupLatency            metric.Float64Histogram
		CatchupReplayNumBlocks    metric.Int64Counter
		SnapshotWriteLatency      metric.Float64Histogram
		SnapshotPruneLatency      metric.Float64Histogram
		SnapshotPruneAttempts     metric.Int64Counter
		CurrentSnapshotHeight     metric.Int64Gauge
		RollbackLatency           metric.Float64Histogram
		ImportLatency             metric.Float64Histogram
		ImportKVPairs             metric.Int64Counter
		ImportWorkerFlushLatency  metric.Float64Histogram
		FlushLatency              metric.Float64Histogram
	}{
		OpenLatency: must(flatkvMeter.Float64Histogram(
			"flatkv_open_latency",
			metric.WithDescription("Time taken to open the FlatKV store"),
			metric.WithUnit("s"),
		)),
		ApplyChangesetsLatency: must(flatkvMeter.Float64Histogram(
			"flatkv_apply_changesets_latency",
			metric.WithDescription("Time taken to apply changesets to FlatKV"),
			metric.WithUnit("s"),
		)),
		CommitLatency: must(flatkvMeter.Float64Histogram(
			"flatkv_commit_latency",
			metric.WithDescription("Time taken to commit FlatKV changes"),
			metric.WithUnit("s"),
		)),
		CommitBatchLatency: must(flatkvMeter.Float64Histogram(
			"flatkv_commit_batch_latency",
			metric.WithDescription("Time taken to commit a FlatKV data DB batch"),
			metric.WithUnit("s"),
		)),
		BatchReadOldValuesLatency: must(flatkvMeter.Float64Histogram(
			"flatkv_batch_read_old_values_latency",
			metric.WithDescription("Time taken to batch read old FlatKV values"),
			metric.WithUnit("s"),
		)),
		NumKVPairs: must(flatkvMeter.Int64Counter(
			"flatkv_num_kv_pairs",
			metric.WithDescription("Number of key-value pairs applied to FlatKV"),
			metric.WithUnit("{count}"),
		)),
		PendingWrites: must(flatkvMeter.Int64Gauge(
			"flatkv_pending_writes",
			metric.WithDescription("Current number of pending FlatKV writes"),
			metric.WithUnit("{count}"),
		)),
		CurrentVersion: must(flatkvMeter.Int64Gauge(
			"flatkv_current_version",
			metric.WithDescription("Current committed FlatKV version"),
			metric.WithUnit("{count}"),
		)),
		CatchupLatency: must(flatkvMeter.Float64Histogram(
			"flatkv_catchup_latency",
			metric.WithDescription("Time taken to replay FlatKV WAL entries"),
			metric.WithUnit("s"),
		)),
		CatchupReplayNumBlocks: must(flatkvMeter.Int64Counter(
			"flatkv_catchup_replay_num_blocks",
			metric.WithDescription("Number of FlatKV WAL entries replayed during catchup"),
			metric.WithUnit("{count}"),
		)),
		SnapshotWriteLatency: must(flatkvMeter.Float64Histogram(
			"flatkv_snapshot_write_latency",
			metric.WithDescription("Time taken to write a FlatKV snapshot"),
			metric.WithUnit("s"),
		)),
		SnapshotPruneLatency: must(flatkvMeter.Float64Histogram(
			"flatkv_snapshot_prune_latency",
			metric.WithDescription("Time taken to prune FlatKV snapshots"),
			metric.WithUnit("s"),
		)),
		SnapshotPruneAttempts: must(flatkvMeter.Int64Counter(
			"flatkv_snapshot_prune_attempts",
			metric.WithDescription("Total number of FlatKV snapshot prune attempts"),
			metric.WithUnit("{count}"),
		)),
		CurrentSnapshotHeight: must(flatkvMeter.Int64Gauge(
			"flatkv_current_snapshot_height",
			metric.WithDescription("Current FlatKV snapshot height"),
			metric.WithUnit("{count}"),
		)),
		RollbackLatency: must(flatkvMeter.Float64Histogram(
			"flatkv_rollback_latency",
			metric.WithDescription("Time taken to rollback FlatKV state"),
			metric.WithUnit("s"),
		)),
		ImportLatency: must(flatkvMeter.Float64Histogram(
			"flatkv_import_latency",
			metric.WithDescription("Time taken to import FlatKV snapshot data"),
			metric.WithUnit("s"),
		)),
		ImportKVPairs: must(flatkvMeter.Int64Counter(
			"flatkv_import_kv_pairs",
			metric.WithDescription("Number of key-value pairs imported into FlatKV"),
			metric.WithUnit("{count}"),
		)),
		ImportWorkerFlushLatency: must(flatkvMeter.Float64Histogram(
			"flatkv_import_worker_flush_latency",
			metric.WithDescription("Time taken to flush a FlatKV import worker batch"),
			metric.WithUnit("s"),
		)),
		FlushLatency: must(flatkvMeter.Float64Histogram(
			"flatkv_flush_latency",
			metric.WithDescription("Time taken to flush a FlatKV data DB"),
			metric.WithUnit("s"),
		)),
	}
)

func must[V any](v V, err error) V {
	if err != nil {
		panic(err)
	}
	return v
}

func secondsSince(start time.Time) float64 {
	return time.Since(start).Seconds()
}

func successAttr(err error) attribute.KeyValue {
	return attribute.Bool("success", err == nil)
}

func dbAttr(db string) attribute.KeyValue {
	return attribute.String("db", db)
}

func recordPendingWrites(ctx context.Context, db string, count int) {
	otelMetrics.PendingWrites.Record(ctx, int64(count), metric.WithAttributes(dbAttr(db)))
}

func addKVPairs(ctx context.Context, db string, count int) {
	if count > 0 {
		otelMetrics.NumKVPairs.Add(ctx, int64(count), metric.WithAttributes(dbAttr(db)))
	}
}

func addImportKVPairs(ctx context.Context, db string, count int) {
	if count > 0 {
		otelMetrics.ImportKVPairs.Add(ctx, int64(count), metric.WithAttributes(dbAttr(db)))
	}
}
