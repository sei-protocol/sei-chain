package flatkv

import (
	"context"
	"errors"
	"time"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/metric"

	"github.com/sei-protocol/sei-chain/sei-db/state_db/sc/flatkv/lthash"
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
		ModuleKeyCount            metric.Int64Gauge
		ModuleTotalBytes          metric.Int64Gauge
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
		ModuleKeyCount: must(flatkvMeter.Int64Gauge(
			"flatkv_module_key_count",
			metric.WithDescription("Current number of live keys for a (data DB, module) pair"),
			metric.WithUnit("{count}"),
		)),
		ModuleTotalBytes: must(flatkvMeter.Int64Gauge(
			"flatkv_module_total_bytes",
			metric.WithDescription("Current total serialized key+value bytes for a (data DB, module) pair"),
			metric.WithUnit("By"),
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

func moduleAttr(module string) attribute.KeyValue {
	return attribute.String("module", module)
}

// recordModuleStats records the current per-module key-count / byte totals for one data DB. Only called
// from CommitStore.recordAllModuleStats, which is itself only reached from Commit's live success path
// (see its doc comment). stats is keyed by module name, mirroring LocalMeta.ModuleStats; a nil/empty map
// is a no-op (fresh DB with no modules yet).
func recordModuleStats(ctx context.Context, db string, stats map[string]lthash.ModuleStats) {
	for module, s := range stats {
		attrs := metric.WithAttributes(dbAttr(db), moduleAttr(module))
		otelMetrics.ModuleKeyCount.Record(ctx, s.KeyCount, attrs)
		otelMetrics.ModuleTotalBytes.Record(ctx, s.Bytes, attrs)
	}
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

// opObserver records latency, success/failure attribution, and an error log
// for a long-running CommitStore operation. Construct one with
// CommitStore.observeOp at the top of the operation, then invoke done from a
// deferred closure so the captured *error reflects the final outcome.
type opObserver struct {
	s          *CommitStore
	op         string
	latency    metric.Float64Histogram
	start      time.Time
	extraAttrs []attribute.KeyValue
	errFields  []any
}

// observeOp captures the start time for an operation. The op name is
// interpolated into the error log message ("FlatKV <op> failed"). errFields
// are emitted in the error log alongside elapsed and err.
func (s *CommitStore) observeOp(
	op string,
	latency metric.Float64Histogram,
	errFields ...any,
) *opObserver {
	return &opObserver{
		s:         s,
		op:        op,
		latency:   latency,
		start:     time.Now(),
		errFields: errFields,
	}
}

// withAttrs adds extra attributes recorded on the latency metric. The
// success/failure attribute is always appended automatically.
func (o *opObserver) withAttrs(attrs ...attribute.KeyValue) *opObserver {
	o.extraAttrs = append(o.extraAttrs, attrs...)
	return o
}

// elapsed returns the time since the operation started. Useful for callers
// that want to include the elapsed value in their own success-path logs.
func (o *opObserver) elapsed() time.Duration {
	return time.Since(o.start)
}

// done records the latency metric and:
//   - on success, invokes onSuccess (if non-nil) for success-only metrics
//     and logs;
//   - on errReadOnly, suppresses the error log (callers explicitly returning
//     errReadOnly are not real failures);
//   - otherwise, logs an error including the static errFields, any dynamic
//     extraErrFields supplied at call time, plus elapsed and err.
//
// extraErrFields lets callers append fields whose values are only known at
// done time (e.g. counters or offsets mutated during the operation). Pass
// values, not pointers — slog formats pointers as their address.
//
// errPtr is dereferenced when done runs, so the typical pattern is to pass
// the address of a named return error from a deferred closure.
func (o *opObserver) done(errPtr *error, onSuccess func(), extraErrFields ...any) {
	err := *errPtr
	attrs := make([]attribute.KeyValue, 0, len(o.extraAttrs)+1)
	attrs = append(attrs, o.extraAttrs...)
	attrs = append(attrs, successAttr(err))
	o.latency.Record(o.s.ctx, secondsSince(o.start),
		metric.WithAttributes(attrs...))
	if err == nil {
		if onSuccess != nil {
			onSuccess()
		}
		return
	}
	if errors.Is(err, errReadOnly) {
		return
	}
	fields := make([]any, 0, len(o.errFields)+len(extraErrFields)+4)
	fields = append(fields, o.errFields...)
	fields = append(fields, extraErrFields...)
	fields = append(fields, "elapsed", o.elapsed(), "err", err)
	logger.Error("FlatKV "+o.op+" failed", fields...)
}
