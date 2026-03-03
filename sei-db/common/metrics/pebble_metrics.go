// Package metrics provides OpenTelemetry instruments and scrapers for Pebble DB metrics,
// allowing any Pebble instance to export compaction, flush, cache, and storage metrics
// to OTel-compatible backends (e.g., Prometheus).
package metrics

import (
	"context"
	"math"
	"time"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/metric"

	"github.com/cockroachdb/pebble/v2"
)

const pebbleMeterName = "seidb_pebble"

// OtelMetrics holds OpenTelemetry instruments for Pebble DB metrics.
// Names, descriptions, and units match sei-db/db_engine/pebbledb/mvcc for dashboard compatibility.
var OtelMetrics = struct {
	GetLatency                 metric.Float64Histogram
	ApplyChangesetLatency      metric.Float64Histogram
	ApplyChangesetAsyncLatency metric.Float64Histogram
	PruneLatency               metric.Float64Histogram
	ImportLatency              metric.Float64Histogram
	BatchWriteLatency          metric.Float64Histogram

	CompactionCount           metric.Int64Counter
	CompactionDuration        metric.Float64Histogram
	CompactionBytesRead       metric.Int64Counter
	CompactionBytesWritten    metric.Int64Counter
	CompactionEstimatedDebt   metric.Int64Gauge
	CompactionInProgressBytes metric.Int64Gauge
	CompactionNumInProgress   metric.Int64Gauge
	CompactionCancelledCount  metric.Int64Counter
	CompactionCancelledBytes  metric.Int64Counter
	CompactionFailedCount     metric.Int64Counter

	IngestCount metric.Int64Counter

	FlushCount              metric.Int64Counter
	FlushDuration           metric.Float64Histogram
	FlushBytesWritten       metric.Int64Counter
	FlushNumInProgress      metric.Int64Gauge
	FlushAsIngestCount      metric.Int64Counter
	FlushAsIngestTableCount metric.Int64Counter
	FlushAsIngestBytes      metric.Int64Counter

	SstableCount           metric.Int64Gauge
	SstableTotalSize       metric.Int64Gauge
	SstableSublevels       metric.Int64Gauge
	SstableScore           metric.Float64Gauge
	SstableFillFactor      metric.Float64Gauge
	SstableVirtualCount    metric.Int64Gauge
	SstableVirtualSize     metric.Int64Gauge
	SstableBytesIngested   metric.Int64Counter
	SstableBytesMoved      metric.Int64Counter
	SstableBytesRead       metric.Int64Counter
	SstableBytesFlushed    metric.Int64Counter
	SstableTablesCompacted metric.Int64Counter
	SstableTablesFlushed   metric.Int64Counter
	SstableTablesIngested  metric.Int64Counter
	SstableTablesMoved     metric.Int64Counter

	MemtableCount       metric.Int64Gauge
	MemtableTotalSize   metric.Int64Gauge
	MemtableZombieSize  metric.Int64Gauge
	MemtableZombieCount metric.Int64Gauge

	WalSize                 metric.Int64Gauge
	WalFiles                metric.Int64Gauge
	WalObsoleteFiles        metric.Int64Gauge
	WalObsoletePhysicalSize metric.Int64Gauge
	WalPhysicalSize         metric.Int64Gauge
	WalBytesIn              metric.Int64Counter
	WalBytesWritten         metric.Int64Counter

	TableObsoleteSize  metric.Int64Gauge
	TableObsoleteCount metric.Int64Gauge
	TableZombieSize    metric.Int64Gauge
	TableZombieCount   metric.Int64Gauge
	TableLiveSize      metric.Int64Gauge
	TableLiveCount     metric.Int64Gauge

	KeysRangeKeySetsCount       metric.Int64Gauge
	KeysTombstoneCount          metric.Int64Gauge
	KeysMissizedTombstonesCount metric.Int64Counter

	SnapshotCount      metric.Int64Gauge
	SnapshotPinnedKeys metric.Int64Counter
	SnapshotPinnedSize metric.Int64Counter

	TableIters     metric.Int64Gauge
	UptimeSeconds  metric.Float64Gauge
	ReadAmp        metric.Int64Gauge
	DiskSpaceUsage metric.Int64Gauge

	CacheHits   metric.Int64Counter
	CacheMisses metric.Int64Counter
	CacheSize   metric.Int64Gauge

	BatchSize                metric.Int64Histogram
	PendingChangesQueueDepth metric.Int64Gauge
	IteratorIterations       metric.Float64Histogram
}{
	GetLatency: must(otel.Meter(pebbleMeterName).Float64Histogram(
		"pebble_get_latency",
		metric.WithDescription("Time taken to get a key from PebbleDB"),
		metric.WithUnit("s"),
	)),
	ApplyChangesetLatency: must(otel.Meter(pebbleMeterName).Float64Histogram(
		"pebble_apply_changeset_latency",
		metric.WithDescription("Time taken to apply changeset to PebbleDB"),
		metric.WithUnit("s"),
	)),
	ApplyChangesetAsyncLatency: must(otel.Meter(pebbleMeterName).Float64Histogram(
		"pebble_apply_changeset_async_latency",
		metric.WithDescription("Time taken to queue changeset for async write"),
		metric.WithUnit("s"),
	)),
	PruneLatency: must(otel.Meter(pebbleMeterName).Float64Histogram(
		"pebble_prune_latency",
		metric.WithDescription("Time taken to prune old versions from PebbleDB"),
		metric.WithUnit("s"),
	)),
	ImportLatency: must(otel.Meter(pebbleMeterName).Float64Histogram(
		"pebble_import_latency",
		metric.WithDescription("Time taken to import snapshot data to PebbleDB"),
		metric.WithUnit("s"),
	)),
	BatchWriteLatency: must(otel.Meter(pebbleMeterName).Float64Histogram(
		"pebble_batch_write_latency",
		metric.WithDescription("Time taken to write a batch to PebbleDB"),
		metric.WithUnit("s"),
	)),

	CompactionCount: must(otel.Meter(pebbleMeterName).Int64Counter(
		"pebble_compaction_count",
		metric.WithDescription("Total number of compactions"),
		metric.WithUnit("{count}"),
	)),
	CompactionDuration: must(otel.Meter(pebbleMeterName).Float64Histogram(
		"pebble_compaction_duration",
		metric.WithDescription("Duration of compaction operations"),
		metric.WithUnit("s"),
	)),
	CompactionBytesRead: must(otel.Meter(pebbleMeterName).Int64Counter(
		"pebble_compaction_bytes_read",
		metric.WithDescription("Total bytes read during compaction"),
		metric.WithUnit("By"),
	)),
	CompactionBytesWritten: must(otel.Meter(pebbleMeterName).Int64Counter(
		"pebble_compaction_bytes_written",
		metric.WithDescription("Total bytes written during compaction"),
		metric.WithUnit("By"),
	)),
	CompactionEstimatedDebt: must(otel.Meter(pebbleMeterName).Int64Gauge(
		"pebble_compaction_estimated_debt",
		metric.WithDescription("Estimated bytes to compact for LSM to reach stable state"),
		metric.WithUnit("By"),
	)),
	CompactionInProgressBytes: must(otel.Meter(pebbleMeterName).Int64Gauge(
		"pebble_compaction_in_progress_bytes",
		metric.WithDescription("Bytes in sstables being written by in-progress compactions"),
		metric.WithUnit("By"),
	)),
	CompactionNumInProgress: must(otel.Meter(pebbleMeterName).Int64Gauge(
		"pebble_compaction_num_in_progress",
		metric.WithDescription("Number of compactions in progress"),
		metric.WithUnit("{count}"),
	)),
	CompactionCancelledCount: must(otel.Meter(pebbleMeterName).Int64Counter(
		"pebble_compaction_cancelled_count",
		metric.WithDescription("Number of compactions that were cancelled"),
		metric.WithUnit("{count}"),
	)),
	CompactionCancelledBytes: must(otel.Meter(pebbleMeterName).Int64Counter(
		"pebble_compaction_cancelled_bytes",
		metric.WithDescription("Bytes written by cancelled compactions"),
		metric.WithUnit("By"),
	)),
	CompactionFailedCount: must(otel.Meter(pebbleMeterName).Int64Counter(
		"pebble_compaction_failed_count",
		metric.WithDescription("Number of compactions that hit an error"),
		metric.WithUnit("{count}"),
	)),

	IngestCount: must(otel.Meter(pebbleMeterName).Int64Counter(
		"pebble_ingest_count",
		metric.WithDescription("Total number of ingestions"),
		metric.WithUnit("{count}"),
	)),

	FlushCount: must(otel.Meter(pebbleMeterName).Int64Counter(
		"pebble_flush_count",
		metric.WithDescription("Total number of memtable flushes"),
		metric.WithUnit("{count}"),
	)),
	FlushDuration: must(otel.Meter(pebbleMeterName).Float64Histogram(
		"pebble_flush_duration",
		metric.WithDescription("Duration of memtable flush operations"),
		metric.WithUnit("s"),
	)),
	FlushBytesWritten: must(otel.Meter(pebbleMeterName).Int64Counter(
		"pebble_flush_bytes_written",
		metric.WithDescription("Total bytes written during memtable flushes"),
		metric.WithUnit("By"),
	)),
	FlushNumInProgress: must(otel.Meter(pebbleMeterName).Int64Gauge(
		"pebble_flush_num_in_progress",
		metric.WithDescription("Number of flushes in progress"),
		metric.WithUnit("{count}"),
	)),
	FlushAsIngestCount: must(otel.Meter(pebbleMeterName).Int64Counter(
		"pebble_flush_as_ingest_count",
		metric.WithDescription("Flush operations handling ingested tables"),
		metric.WithUnit("{count}"),
	)),
	FlushAsIngestTableCount: must(otel.Meter(pebbleMeterName).Int64Counter(
		"pebble_flush_as_ingest_table_count",
		metric.WithDescription("Tables ingested as flushables"),
		metric.WithUnit("{count}"),
	)),
	FlushAsIngestBytes: must(otel.Meter(pebbleMeterName).Int64Counter(
		"pebble_flush_as_ingest_bytes",
		metric.WithDescription("Bytes flushed for flushables from ingestion"),
		metric.WithUnit("By"),
	)),

	SstableCount: must(otel.Meter(pebbleMeterName).Int64Gauge(
		"pebble_sstable_count",
		metric.WithDescription("Current number of SSTables at each level"),
		metric.WithUnit("{count}"),
	)),
	SstableTotalSize: must(otel.Meter(pebbleMeterName).Int64Gauge(
		"pebble_sstable_total_size",
		metric.WithDescription("Total size of SSTables at each level"),
		metric.WithUnit("By"),
	)),
	SstableSublevels: must(otel.Meter(pebbleMeterName).Int64Gauge(
		"pebble_sstable_sublevels",
		metric.WithDescription("Number of sublevels (read amplification); L0 only has non-0/1"),
		metric.WithUnit("{count}"),
	)),
	SstableScore: must(otel.Meter(pebbleMeterName).Float64Gauge(
		"pebble_sstable_score",
		metric.WithDescription("Level compaction score (0 if no compaction needed)"),
		metric.WithUnit("1"),
	)),
	SstableFillFactor: must(otel.Meter(pebbleMeterName).Float64Gauge(
		"pebble_sstable_fill_factor",
		metric.WithDescription("Level fill factor (size vs ideal size)"),
		metric.WithUnit("1"),
	)),
	SstableVirtualCount: must(otel.Meter(pebbleMeterName).Int64Gauge(
		"pebble_sstable_virtual_count",
		metric.WithDescription("Number of virtual sstables at level"),
		metric.WithUnit("{count}"),
	)),
	SstableVirtualSize: must(otel.Meter(pebbleMeterName).Int64Gauge(
		"pebble_sstable_virtual_size",
		metric.WithDescription("Size of virtual sstables at level"),
		metric.WithUnit("By"),
	)),
	SstableBytesIngested: must(otel.Meter(pebbleMeterName).Int64Counter(
		"pebble_sstable_bytes_ingested",
		metric.WithDescription("Sstable bytes ingested at level"),
		metric.WithUnit("By"),
	)),
	SstableBytesMoved: must(otel.Meter(pebbleMeterName).Int64Counter(
		"pebble_sstable_bytes_moved",
		metric.WithDescription("Sstable bytes moved by move compaction at level"),
		metric.WithUnit("By"),
	)),
	SstableBytesRead: must(otel.Meter(pebbleMeterName).Int64Counter(
		"pebble_sstable_bytes_read",
		metric.WithDescription("Bytes read for compactions at level"),
		metric.WithUnit("By"),
	)),
	SstableBytesFlushed: must(otel.Meter(pebbleMeterName).Int64Counter(
		"pebble_sstable_bytes_flushed",
		metric.WithDescription("Bytes written to sstables during flushes at level"),
		metric.WithUnit("By"),
	)),
	SstableTablesCompacted: must(otel.Meter(pebbleMeterName).Int64Counter(
		"pebble_sstable_tables_compacted",
		metric.WithDescription("Sstables compacted to this level"),
		metric.WithUnit("{count}"),
	)),
	SstableTablesFlushed: must(otel.Meter(pebbleMeterName).Int64Counter(
		"pebble_sstable_tables_flushed",
		metric.WithDescription("Sstables flushed to this level"),
		metric.WithUnit("{count}"),
	)),
	SstableTablesIngested: must(otel.Meter(pebbleMeterName).Int64Counter(
		"pebble_sstable_tables_ingested",
		metric.WithDescription("Sstables ingested into level"),
		metric.WithUnit("{count}"),
	)),
	SstableTablesMoved: must(otel.Meter(pebbleMeterName).Int64Counter(
		"pebble_sstable_tables_moved",
		metric.WithDescription("Sstables moved to level by move compaction"),
		metric.WithUnit("{count}"),
	)),

	MemtableCount: must(otel.Meter(pebbleMeterName).Int64Gauge(
		"pebble_memtable_count",
		metric.WithDescription("Current number of memtables"),
		metric.WithUnit("{count}"),
	)),
	MemtableTotalSize: must(otel.Meter(pebbleMeterName).Int64Gauge(
		"pebble_memtable_total_size",
		metric.WithDescription("Total size of all memtables"),
		metric.WithUnit("By"),
	)),
	MemtableZombieSize: must(otel.Meter(pebbleMeterName).Int64Gauge(
		"pebble_memtable_zombie_size",
		metric.WithDescription("Bytes in zombie memtables (released but in use by iterators)"),
		metric.WithUnit("By"),
	)),
	MemtableZombieCount: must(otel.Meter(pebbleMeterName).Int64Gauge(
		"pebble_memtable_zombie_count",
		metric.WithDescription("Count of zombie memtables"),
		metric.WithUnit("{count}"),
	)),
	WalSize: must(otel.Meter(pebbleMeterName).Int64Gauge(
		"pebble_wal_size",
		metric.WithDescription("Current size of Write-Ahead Log"),
		metric.WithUnit("By"),
	)),
	WalFiles: must(otel.Meter(pebbleMeterName).Int64Gauge(
		"pebble_wal_files",
		metric.WithDescription("Number of live WAL files"),
		metric.WithUnit("{count}"),
	)),
	WalObsoleteFiles: must(otel.Meter(pebbleMeterName).Int64Gauge(
		"pebble_wal_obsolete_files",
		metric.WithDescription("Number of obsolete WAL files"),
		metric.WithUnit("{count}"),
	)),
	WalObsoletePhysicalSize: must(otel.Meter(pebbleMeterName).Int64Gauge(
		"pebble_wal_obsolete_physical_size",
		metric.WithDescription("Physical size of obsolete WAL files"),
		metric.WithUnit("By"),
	)),
	WalPhysicalSize: must(otel.Meter(pebbleMeterName).Int64Gauge(
		"pebble_wal_physical_size",
		metric.WithDescription("Physical size of WAL files on disk"),
		metric.WithUnit("By"),
	)),
	WalBytesIn: must(otel.Meter(pebbleMeterName).Int64Counter(
		"pebble_wal_bytes_in",
		metric.WithDescription("Logical bytes written to WAL"),
		metric.WithUnit("By"),
	)),
	WalBytesWritten: must(otel.Meter(pebbleMeterName).Int64Counter(
		"pebble_wal_bytes_written",
		metric.WithDescription("Bytes written to WAL"),
		metric.WithUnit("By"),
	)),

	TableObsoleteSize: must(otel.Meter(pebbleMeterName).Int64Gauge(
		"pebble_table_obsolete_size",
		metric.WithDescription("Bytes in obsolete tables no longer referenced"),
		metric.WithUnit("By"),
	)),
	TableObsoleteCount: must(otel.Meter(pebbleMeterName).Int64Gauge(
		"pebble_table_obsolete_count",
		metric.WithDescription("Count of obsolete tables"),
		metric.WithUnit("{count}"),
	)),
	TableZombieSize: must(otel.Meter(pebbleMeterName).Int64Gauge(
		"pebble_table_zombie_size",
		metric.WithDescription("Bytes in zombie tables (released but in use by iterators)"),
		metric.WithUnit("By"),
	)),
	TableZombieCount: must(otel.Meter(pebbleMeterName).Int64Gauge(
		"pebble_table_zombie_count",
		metric.WithDescription("Count of zombie tables"),
		metric.WithUnit("{count}"),
	)),
	TableLiveSize: must(otel.Meter(pebbleMeterName).Int64Gauge(
		"pebble_table_live_size",
		metric.WithDescription("Bytes in live tables"),
		metric.WithUnit("By"),
	)),
	TableLiveCount: must(otel.Meter(pebbleMeterName).Int64Gauge(
		"pebble_table_live_count",
		metric.WithDescription("Count of live tables"),
		metric.WithUnit("{count}"),
	)),

	KeysRangeKeySetsCount: must(otel.Meter(pebbleMeterName).Int64Gauge(
		"pebble_keys_range_key_sets_count",
		metric.WithDescription("Approximate count of internal range key set keys"),
		metric.WithUnit("{count}"),
	)),
	KeysTombstoneCount: must(otel.Meter(pebbleMeterName).Int64Gauge(
		"pebble_keys_tombstone_count",
		metric.WithDescription("Approximate count of internal tombstones"),
		metric.WithUnit("{count}"),
	)),
	KeysMissizedTombstonesCount: must(otel.Meter(pebbleMeterName).Int64Counter(
		"pebble_keys_missized_tombstones_count",
		metric.WithDescription("Missized DELSIZED keys encountered by compactions"),
		metric.WithUnit("{count}"),
	)),

	SnapshotCount: must(otel.Meter(pebbleMeterName).Int64Gauge(
		"pebble_snapshot_count",
		metric.WithDescription("Number of currently open snapshots"),
		metric.WithUnit("{count}"),
	)),
	SnapshotPinnedKeys: must(otel.Meter(pebbleMeterName).Int64Counter(
		"pebble_snapshot_pinned_keys",
		metric.WithDescription("Keys written that would've been elided without open snapshots"),
		metric.WithUnit("{count}"),
	)),
	SnapshotPinnedSize: must(otel.Meter(pebbleMeterName).Int64Counter(
		"pebble_snapshot_pinned_size",
		metric.WithDescription("Size of keys/values written due to open snapshots"),
		metric.WithUnit("By"),
	)),

	TableIters: must(otel.Meter(pebbleMeterName).Int64Gauge(
		"pebble_table_iters",
		metric.WithDescription("Count of open sstable iterators"),
		metric.WithUnit("{count}"),
	)),
	UptimeSeconds: must(otel.Meter(pebbleMeterName).Float64Gauge(
		"pebble_uptime_seconds",
		metric.WithDescription("Seconds since DB was opened"),
		metric.WithUnit("s"),
	)),
	ReadAmp: must(otel.Meter(pebbleMeterName).Int64Gauge(
		"pebble_read_amp",
		metric.WithDescription("Read amplification"),
		metric.WithUnit("{count}"),
	)),
	DiskSpaceUsage: must(otel.Meter(pebbleMeterName).Int64Gauge(
		"pebble_disk_space_usage",
		metric.WithDescription("Total disk space used by the DB"),
		metric.WithUnit("By"),
	)),

	CacheHits: must(otel.Meter(pebbleMeterName).Int64Counter(
		"pebble_cache_hits",
		metric.WithDescription("Total number of cache hits"),
		metric.WithUnit("{count}"),
	)),
	CacheMisses: must(otel.Meter(pebbleMeterName).Int64Counter(
		"pebble_cache_misses",
		metric.WithDescription("Total number of cache misses"),
		metric.WithUnit("{count}"),
	)),
	CacheSize: must(otel.Meter(pebbleMeterName).Int64Gauge(
		"pebble_cache_size",
		metric.WithDescription("Current cache size"),
		metric.WithUnit("By"),
	)),

	BatchSize: must(otel.Meter(pebbleMeterName).Int64Histogram(
		"pebble_batch_size",
		metric.WithDescription("Size of batches written to PebbleDB"),
		metric.WithUnit("By"),
	)),
	PendingChangesQueueDepth: must(otel.Meter(pebbleMeterName).Int64Gauge(
		"pebble_pending_changes_queue_depth",
		metric.WithDescription("Number of pending changesets in async write queue"),
		metric.WithUnit("{count}"),
	)),
	IteratorIterations: must(otel.Meter(pebbleMeterName).Float64Histogram(
		"pebble_iterator_iterations",
		metric.WithDescription("Number of iterations per iterator"),
		metric.WithUnit("{count}"),
	)),
}

// must panics if err is non-nil, otherwise returns v. Used when initializing
// OTel instruments at startup; instrument creation failures are considered fatal.
func must[V any](v V, err error) V {
	if err != nil {
		panic(err)
	}
	return v
}

// uint64ToInt64Clamped converts a uint64 to int64, clamping to math.MaxInt64 to avoid overflow.
func uint64ToInt64Clamped(v uint64) int64 {
	if v > math.MaxInt64 {
		return math.MaxInt64
	}
	return int64(v)
}

// PebbleMetrics scrapes metrics from a Pebble DB and records them to OtelMetrics.
// The databaseName is used as the "db" attribute on all metrics for multi-DB setups.
type PebbleMetrics struct {
	db           *pebble.DB
	databaseName string
}

// NewPebbleMetrics creates a PebbleMetrics that scrapes metrics from the given Pebble DB
// and records them to OtelMetrics. A background goroutine runs every scrapeInterval until
// ctx is cancelled. The databaseName is attached as the "db" attribute to all recorded
// metrics, enabling multi-DB setups to distinguish series in Prometheus/Grafana.
func NewPebbleMetrics(
	ctx context.Context,
	db *pebble.DB,
	databaseName string,
	scrapeInterval time.Duration,
) *PebbleMetrics {
	pm := &PebbleMetrics{db: db, databaseName: databaseName}
	go pm.collectLoop(ctx, scrapeInterval)
	return pm
}

// collectLoop runs a ticker that periodically calls recordFromPebble. It exits when ctx is cancelled.
func (pm *PebbleMetrics) collectLoop(ctx context.Context, interval time.Duration) {
	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			pm.recordFromPebble(ctx)
		}
	}
}

// recordFromPebble fetches the current metrics from the Pebble DB via Metrics(), then
// records compaction, flush, level, memtable, WAL, and cache metrics into OtelMetrics
// with the configured database name as the "db" attribute.
func (pm *PebbleMetrics) recordFromPebble(ctx context.Context) {
	if pm.db == nil {
		return
	}
	m := pm.db.Metrics()
	dbAttr := attribute.String("db", pm.databaseName)

	// Compact
	OtelMetrics.CompactionCount.Add(ctx, m.Compact.Count, metric.WithAttributes(dbAttr))
	OtelMetrics.CompactionDuration.Record(ctx, m.Compact.Duration.Seconds(), metric.WithAttributes(dbAttr))
	OtelMetrics.CompactionEstimatedDebt.Record(ctx,
		uint64ToInt64Clamped(m.Compact.EstimatedDebt), metric.WithAttributes(dbAttr))
	OtelMetrics.CompactionInProgressBytes.Record(ctx, m.Compact.InProgressBytes, metric.WithAttributes(dbAttr))
	OtelMetrics.CompactionNumInProgress.Record(ctx, m.Compact.NumInProgress, metric.WithAttributes(dbAttr))
	OtelMetrics.CompactionCancelledCount.Add(ctx, m.Compact.CancelledCount, metric.WithAttributes(dbAttr))
	OtelMetrics.CompactionCancelledBytes.Add(ctx, m.Compact.CancelledBytes, metric.WithAttributes(dbAttr))
	OtelMetrics.CompactionFailedCount.Add(ctx, m.Compact.FailedCount, metric.WithAttributes(dbAttr))

	// Ingest
	OtelMetrics.IngestCount.Add(ctx, int64(m.Ingest.Count), metric.WithAttributes(dbAttr))

	// Flush
	OtelMetrics.FlushCount.Add(ctx, m.Flush.Count, metric.WithAttributes(dbAttr))
	OtelMetrics.FlushDuration.Record(ctx,
		m.Flush.WriteThroughput.WorkDuration.Seconds(), metric.WithAttributes(dbAttr))
	OtelMetrics.FlushBytesWritten.Add(ctx, m.Flush.WriteThroughput.Bytes, metric.WithAttributes(dbAttr))
	OtelMetrics.FlushNumInProgress.Record(ctx, m.Flush.NumInProgress, metric.WithAttributes(dbAttr))
	OtelMetrics.FlushAsIngestCount.Add(ctx, int64(m.Flush.AsIngestCount), metric.WithAttributes(dbAttr))
	OtelMetrics.FlushAsIngestTableCount.Add(ctx, int64(m.Flush.AsIngestTableCount), metric.WithAttributes(dbAttr))
	OtelMetrics.FlushAsIngestBytes.Add(ctx,
		uint64ToInt64Clamped(m.Flush.AsIngestBytes), metric.WithAttributes(dbAttr))

	// Levels
	for level := 0; level < len(m.Levels); level++ {
		lm := m.Levels[level]
		levelAttr := attribute.Int("level", level)
		attrs := metric.WithAttributes(dbAttr, levelAttr)

		OtelMetrics.SstableCount.Record(ctx, lm.TablesCount, attrs)
		OtelMetrics.SstableTotalSize.Record(ctx, lm.TablesSize, attrs)
		OtelMetrics.SstableSublevels.Record(ctx, int64(lm.Sublevels), attrs)
		OtelMetrics.SstableScore.Record(ctx, lm.Score, attrs)
		OtelMetrics.SstableFillFactor.Record(ctx, lm.FillFactor, attrs)
		OtelMetrics.SstableVirtualCount.Record(ctx, int64(lm.VirtualTablesCount), attrs)
		OtelMetrics.SstableVirtualSize.Record(ctx, uint64ToInt64Clamped(lm.VirtualTablesSize), attrs)
		OtelMetrics.CompactionBytesRead.Add(ctx, int64(lm.TableBytesIn), attrs)
		OtelMetrics.CompactionBytesWritten.Add(ctx, int64(lm.TableBytesCompacted), attrs)
		OtelMetrics.SstableBytesIngested.Add(ctx, uint64ToInt64Clamped(lm.TableBytesIngested), attrs)
		OtelMetrics.SstableBytesMoved.Add(ctx, uint64ToInt64Clamped(lm.TableBytesMoved), attrs)
		OtelMetrics.SstableBytesRead.Add(ctx, uint64ToInt64Clamped(lm.TableBytesRead), attrs)
		OtelMetrics.SstableBytesFlushed.Add(ctx, uint64ToInt64Clamped(lm.TableBytesFlushed), attrs)
		OtelMetrics.SstableTablesCompacted.Add(ctx, uint64ToInt64Clamped(lm.TablesCompacted), attrs)
		OtelMetrics.SstableTablesFlushed.Add(ctx, uint64ToInt64Clamped(lm.TablesFlushed), attrs)
		OtelMetrics.SstableTablesIngested.Add(ctx, uint64ToInt64Clamped(lm.TablesIngested), attrs)
		OtelMetrics.SstableTablesMoved.Add(ctx, uint64ToInt64Clamped(lm.TablesMoved), attrs)
	}

	// MemTable
	OtelMetrics.MemtableCount.Record(ctx, m.MemTable.Count, metric.WithAttributes(dbAttr))
	OtelMetrics.MemtableTotalSize.Record(ctx, int64(m.MemTable.Size), metric.WithAttributes(dbAttr))
	OtelMetrics.MemtableZombieSize.Record(ctx,
		uint64ToInt64Clamped(m.MemTable.ZombieSize), metric.WithAttributes(dbAttr))
	OtelMetrics.MemtableZombieCount.Record(ctx, m.MemTable.ZombieCount, metric.WithAttributes(dbAttr))

	// WAL
	OtelMetrics.WalSize.Record(ctx, int64(m.WAL.Size), metric.WithAttributes(dbAttr))
	OtelMetrics.WalFiles.Record(ctx, m.WAL.Files, metric.WithAttributes(dbAttr))
	OtelMetrics.WalObsoleteFiles.Record(ctx, m.WAL.ObsoleteFiles, metric.WithAttributes(dbAttr))
	OtelMetrics.WalObsoletePhysicalSize.Record(ctx,
		uint64ToInt64Clamped(m.WAL.ObsoletePhysicalSize), metric.WithAttributes(dbAttr))
	OtelMetrics.WalPhysicalSize.Record(ctx, uint64ToInt64Clamped(m.WAL.PhysicalSize), metric.WithAttributes(dbAttr))
	OtelMetrics.WalBytesIn.Add(ctx, uint64ToInt64Clamped(m.WAL.BytesIn), metric.WithAttributes(dbAttr))
	OtelMetrics.WalBytesWritten.Add(ctx, uint64ToInt64Clamped(m.WAL.BytesWritten), metric.WithAttributes(dbAttr))

	// Table
	OtelMetrics.TableObsoleteSize.Record(ctx,
		uint64ToInt64Clamped(m.Table.ObsoleteSize), metric.WithAttributes(dbAttr))
	OtelMetrics.TableObsoleteCount.Record(ctx, m.Table.ObsoleteCount, metric.WithAttributes(dbAttr))
	OtelMetrics.TableZombieSize.Record(ctx,
		uint64ToInt64Clamped(m.Table.ZombieSize), metric.WithAttributes(dbAttr))
	OtelMetrics.TableZombieCount.Record(ctx, m.Table.ZombieCount, metric.WithAttributes(dbAttr))
	OtelMetrics.TableLiveSize.Record(ctx,
		uint64ToInt64Clamped(m.Table.Local.LiveSize), metric.WithAttributes(dbAttr))
	OtelMetrics.TableLiveCount.Record(ctx,
		uint64ToInt64Clamped(m.Table.Local.LiveCount), metric.WithAttributes(dbAttr))

	// Keys
	OtelMetrics.KeysRangeKeySetsCount.Record(ctx,
		uint64ToInt64Clamped(m.Keys.RangeKeySetsCount), metric.WithAttributes(dbAttr))
	OtelMetrics.KeysTombstoneCount.Record(ctx,
		uint64ToInt64Clamped(m.Keys.TombstoneCount), metric.WithAttributes(dbAttr))
	OtelMetrics.KeysMissizedTombstonesCount.Add(ctx,
		uint64ToInt64Clamped(m.Keys.MissizedTombstonesCount), metric.WithAttributes(dbAttr))

	// Snapshots
	OtelMetrics.SnapshotCount.Record(ctx, int64(m.Snapshots.Count), metric.WithAttributes(dbAttr))
	OtelMetrics.SnapshotPinnedKeys.Add(ctx,
		uint64ToInt64Clamped(m.Snapshots.PinnedKeys), metric.WithAttributes(dbAttr))
	OtelMetrics.SnapshotPinnedSize.Add(ctx,
		uint64ToInt64Clamped(m.Snapshots.PinnedSize), metric.WithAttributes(dbAttr))

	// Top-level
	OtelMetrics.TableIters.Record(ctx, m.TableIters, metric.WithAttributes(dbAttr))
	OtelMetrics.UptimeSeconds.Record(ctx, m.Uptime.Seconds(), metric.WithAttributes(dbAttr))
	OtelMetrics.ReadAmp.Record(ctx, int64(m.ReadAmp()), metric.WithAttributes(dbAttr))
	OtelMetrics.DiskSpaceUsage.Record(ctx, uint64ToInt64Clamped(m.DiskSpaceUsage()), metric.WithAttributes(dbAttr))

	// Cache
	OtelMetrics.CacheHits.Add(ctx, m.BlockCache.Hits, metric.WithAttributes(dbAttr))
	OtelMetrics.CacheMisses.Add(ctx, m.BlockCache.Misses, metric.WithAttributes(dbAttr))
	OtelMetrics.CacheSize.Record(ctx, m.BlockCache.Size, metric.WithAttributes(dbAttr))
}
