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

// PebbleMetrics scrapes metrics from a Pebble DB and records them via OTel instruments.
// Instrument names match sei-db/db_engine/pebbledb/mvcc for dashboard compatibility.
// The databaseName is used as the "db" attribute on all recorded metrics.
//
// Multiple instances are safe: OTel instrument registration is idempotent, so each
// NewPebbleMetrics call receives references to the same underlying instruments.
// The "db" attribute distinguishes series (e.g. pebble_compaction_count{db="state"}).
type PebbleMetrics struct {
	db           *pebble.DB
	databaseName string

	getLatency                 metric.Float64Histogram
	applyChangesetLatency      metric.Float64Histogram
	applyChangesetAsyncLatency metric.Float64Histogram
	pruneLatency               metric.Float64Histogram
	importLatency              metric.Float64Histogram
	batchWriteLatency          metric.Float64Histogram

	compactionCount           metric.Int64Counter
	compactionDuration        metric.Float64Histogram
	compactionBytesRead       metric.Int64Counter
	compactionBytesWritten    metric.Int64Counter
	compactionEstimatedDebt   metric.Int64Gauge
	compactionInProgressBytes metric.Int64Gauge
	compactionNumInProgress   metric.Int64Gauge
	compactionCancelledCount  metric.Int64Counter
	compactionCancelledBytes  metric.Int64Counter
	compactionFailedCount     metric.Int64Counter

	ingestCount metric.Int64Counter

	flushCount              metric.Int64Counter
	flushDuration           metric.Float64Histogram
	flushBytesWritten       metric.Int64Counter
	flushNumInProgress      metric.Int64Gauge
	flushAsIngestCount      metric.Int64Counter
	flushAsIngestTableCount metric.Int64Counter
	flushAsIngestBytes      metric.Int64Counter

	sstableCount           metric.Int64Gauge
	sstableTotalSize       metric.Int64Gauge
	sstableSublevels       metric.Int64Gauge
	sstableScore           metric.Float64Gauge
	sstableFillFactor      metric.Float64Gauge
	sstableVirtualCount    metric.Int64Gauge
	sstableVirtualSize     metric.Int64Gauge
	sstableBytesIngested   metric.Int64Counter
	sstableBytesMoved      metric.Int64Counter
	sstableBytesRead       metric.Int64Counter
	sstableBytesFlushed    metric.Int64Counter
	sstableTablesCompacted metric.Int64Counter
	sstableTablesFlushed   metric.Int64Counter
	sstableTablesIngested  metric.Int64Counter
	sstableTablesMoved     metric.Int64Counter

	memtableCount       metric.Int64Gauge
	memtableTotalSize   metric.Int64Gauge
	memtableZombieSize  metric.Int64Gauge
	memtableZombieCount metric.Int64Gauge

	walSize                 metric.Int64Gauge
	walFiles                metric.Int64Gauge
	walObsoleteFiles        metric.Int64Gauge
	walObsoletePhysicalSize metric.Int64Gauge
	walPhysicalSize         metric.Int64Gauge
	walBytesIn              metric.Int64Counter
	walBytesWritten         metric.Int64Counter

	tableObsoleteSize  metric.Int64Gauge
	tableObsoleteCount metric.Int64Gauge
	tableZombieSize    metric.Int64Gauge
	tableZombieCount   metric.Int64Gauge
	tableLiveSize      metric.Int64Gauge
	tableLiveCount     metric.Int64Gauge

	keysRangeKeySetsCount       metric.Int64Gauge
	keysTombstoneCount          metric.Int64Gauge
	keysMissizedTombstonesCount metric.Int64Counter

	snapshotCount      metric.Int64Gauge
	snapshotPinnedKeys metric.Int64Counter
	snapshotPinnedSize metric.Int64Counter

	tableIters     metric.Int64Gauge
	uptimeSeconds  metric.Float64Gauge
	readAmp        metric.Int64Gauge
	diskSpaceUsage metric.Int64Gauge

	cacheHits   metric.Int64Counter
	cacheMisses metric.Int64Counter
	cacheSize   metric.Int64Gauge

	batchSize                metric.Int64Histogram
	pendingChangesQueueDepth metric.Int64Gauge
	iteratorIterations       metric.Float64Histogram
}

// NewPebbleMetrics creates a PebbleMetrics that scrapes metrics from the given Pebble DB
// and records them to OTel. A background goroutine runs every scrapeInterval until
// ctx is cancelled. The databaseName is attached as the "db" attribute to all recorded
// metrics, enabling multi-DB setups to distinguish series in Prometheus/Grafana.
//
// Multiple instances (e.g. one per DB) are safe: OTel returns the same instruments
// for duplicate registrations, and the "db" attribute separates series.
func NewPebbleMetrics(
	ctx context.Context,
	db *pebble.DB,
	databaseName string,
	scrapeInterval time.Duration,
) *PebbleMetrics {
	meter := otel.Meter(pebbleMeterName)

	getLatency, _ := meter.Float64Histogram(
		"pebble_get_latency",
		metric.WithDescription("Time taken to get a key from PebbleDB"),
		metric.WithUnit("s"),
	)
	applyChangesetLatency, _ := meter.Float64Histogram(
		"pebble_apply_changeset_latency",
		metric.WithDescription("Time taken to apply changeset to PebbleDB"),
		metric.WithUnit("s"),
	)
	applyChangesetAsyncLatency, _ := meter.Float64Histogram(
		"pebble_apply_changeset_async_latency",
		metric.WithDescription("Time taken to queue changeset for async write"),
		metric.WithUnit("s"),
	)
	pruneLatency, _ := meter.Float64Histogram(
		"pebble_prune_latency",
		metric.WithDescription("Time taken to prune old versions from PebbleDB"),
		metric.WithUnit("s"),
	)
	importLatency, _ := meter.Float64Histogram(
		"pebble_import_latency",
		metric.WithDescription("Time taken to import snapshot data to PebbleDB"),
		metric.WithUnit("s"),
	)
	batchWriteLatency, _ := meter.Float64Histogram(
		"pebble_batch_write_latency",
		metric.WithDescription("Time taken to write a batch to PebbleDB"),
		metric.WithUnit("s"),
	)

	compactionCount, _ := meter.Int64Counter(
		"pebble_compaction_count",
		metric.WithDescription("Total number of compactions"),
		metric.WithUnit("{count}"),
	)
	compactionDuration, _ := meter.Float64Histogram(
		"pebble_compaction_duration",
		metric.WithDescription("Duration of compaction operations"),
		metric.WithUnit("s"),
	)
	compactionBytesRead, _ := meter.Int64Counter(
		"pebble_compaction_bytes_read",
		metric.WithDescription("Total bytes read during compaction"),
		metric.WithUnit("By"),
	)
	compactionBytesWritten, _ := meter.Int64Counter(
		"pebble_compaction_bytes_written",
		metric.WithDescription("Total bytes written during compaction"),
		metric.WithUnit("By"),
	)
	compactionEstimatedDebt, _ := meter.Int64Gauge(
		"pebble_compaction_estimated_debt",
		metric.WithDescription("Estimated bytes to compact for LSM to reach stable state"),
		metric.WithUnit("By"),
	)
	compactionInProgressBytes, _ := meter.Int64Gauge(
		"pebble_compaction_in_progress_bytes",
		metric.WithDescription("Bytes in sstables being written by in-progress compactions"),
		metric.WithUnit("By"),
	)
	compactionNumInProgress, _ := meter.Int64Gauge(
		"pebble_compaction_num_in_progress",
		metric.WithDescription("Number of compactions in progress"),
		metric.WithUnit("{count}"),
	)
	compactionCancelledCount, _ := meter.Int64Counter(
		"pebble_compaction_cancelled_count",
		metric.WithDescription("Number of compactions that were cancelled"),
		metric.WithUnit("{count}"),
	)
	compactionCancelledBytes, _ := meter.Int64Counter(
		"pebble_compaction_cancelled_bytes",
		metric.WithDescription("Bytes written by cancelled compactions"),
		metric.WithUnit("By"),
	)
	compactionFailedCount, _ := meter.Int64Counter(
		"pebble_compaction_failed_count",
		metric.WithDescription("Number of compactions that hit an error"),
		metric.WithUnit("{count}"),
	)

	ingestCount, _ := meter.Int64Counter(
		"pebble_ingest_count",
		metric.WithDescription("Total number of ingestions"),
		metric.WithUnit("{count}"),
	)

	flushCount, _ := meter.Int64Counter(
		"pebble_flush_count",
		metric.WithDescription("Total number of memtable flushes"),
		metric.WithUnit("{count}"),
	)
	flushDuration, _ := meter.Float64Histogram(
		"pebble_flush_duration",
		metric.WithDescription("Duration of memtable flush operations"),
		metric.WithUnit("s"),
	)
	flushBytesWritten, _ := meter.Int64Counter(
		"pebble_flush_bytes_written",
		metric.WithDescription("Total bytes written during memtable flushes"),
		metric.WithUnit("By"),
	)
	flushNumInProgress, _ := meter.Int64Gauge(
		"pebble_flush_num_in_progress",
		metric.WithDescription("Number of flushes in progress"),
		metric.WithUnit("{count}"),
	)
	flushAsIngestCount, _ := meter.Int64Counter(
		"pebble_flush_as_ingest_count",
		metric.WithDescription("Flush operations handling ingested tables"),
		metric.WithUnit("{count}"),
	)
	flushAsIngestTableCount, _ := meter.Int64Counter(
		"pebble_flush_as_ingest_table_count",
		metric.WithDescription("Tables ingested as flushables"),
		metric.WithUnit("{count}"),
	)
	flushAsIngestBytes, _ := meter.Int64Counter(
		"pebble_flush_as_ingest_bytes",
		metric.WithDescription("Bytes flushed for flushables from ingestion"),
		metric.WithUnit("By"),
	)

	sstableCount, _ := meter.Int64Gauge(
		"pebble_sstable_count",
		metric.WithDescription("Current number of SSTables at each level"),
		metric.WithUnit("{count}"),
	)
	sstableTotalSize, _ := meter.Int64Gauge(
		"pebble_sstable_total_size",
		metric.WithDescription("Total size of SSTables at each level"),
		metric.WithUnit("By"),
	)
	sstableSublevels, _ := meter.Int64Gauge(
		"pebble_sstable_sublevels",
		metric.WithDescription("Number of sublevels (read amplification); L0 only has non-0/1"),
		metric.WithUnit("{count}"),
	)
	sstableScore, _ := meter.Float64Gauge(
		"pebble_sstable_score",
		metric.WithDescription("Level compaction score (0 if no compaction needed)"),
		metric.WithUnit("1"),
	)
	sstableFillFactor, _ := meter.Float64Gauge(
		"pebble_sstable_fill_factor",
		metric.WithDescription("Level fill factor (size vs ideal size)"),
		metric.WithUnit("1"),
	)
	sstableVirtualCount, _ := meter.Int64Gauge(
		"pebble_sstable_virtual_count",
		metric.WithDescription("Number of virtual sstables at level"),
		metric.WithUnit("{count}"),
	)
	sstableVirtualSize, _ := meter.Int64Gauge(
		"pebble_sstable_virtual_size",
		metric.WithDescription("Size of virtual sstables at level"),
		metric.WithUnit("By"),
	)
	sstableBytesIngested, _ := meter.Int64Counter(
		"pebble_sstable_bytes_ingested",
		metric.WithDescription("Sstable bytes ingested at level"),
		metric.WithUnit("By"),
	)
	sstableBytesMoved, _ := meter.Int64Counter(
		"pebble_sstable_bytes_moved",
		metric.WithDescription("Sstable bytes moved by move compaction at level"),
		metric.WithUnit("By"),
	)
	sstableBytesRead, _ := meter.Int64Counter(
		"pebble_sstable_bytes_read",
		metric.WithDescription("Bytes read for compactions at level"),
		metric.WithUnit("By"),
	)
	sstableBytesFlushed, _ := meter.Int64Counter(
		"pebble_sstable_bytes_flushed",
		metric.WithDescription("Bytes written to sstables during flushes at level"),
		metric.WithUnit("By"),
	)
	sstableTablesCompacted, _ := meter.Int64Counter(
		"pebble_sstable_tables_compacted",
		metric.WithDescription("Sstables compacted to this level"),
		metric.WithUnit("{count}"),
	)
	sstableTablesFlushed, _ := meter.Int64Counter(
		"pebble_sstable_tables_flushed",
		metric.WithDescription("Sstables flushed to this level"),
		metric.WithUnit("{count}"),
	)
	sstableTablesIngested, _ := meter.Int64Counter(
		"pebble_sstable_tables_ingested",
		metric.WithDescription("Sstables ingested into level"),
		metric.WithUnit("{count}"),
	)
	sstableTablesMoved, _ := meter.Int64Counter(
		"pebble_sstable_tables_moved",
		metric.WithDescription("Sstables moved to level by move compaction"),
		metric.WithUnit("{count}"),
	)

	memtableCount, _ := meter.Int64Gauge(
		"pebble_memtable_count",
		metric.WithDescription("Current number of memtables"),
		metric.WithUnit("{count}"),
	)
	memtableTotalSize, _ := meter.Int64Gauge(
		"pebble_memtable_total_size",
		metric.WithDescription("Total size of all memtables"),
		metric.WithUnit("By"),
	)
	memtableZombieSize, _ := meter.Int64Gauge(
		"pebble_memtable_zombie_size",
		metric.WithDescription("Bytes in zombie memtables (released but in use by iterators)"),
		metric.WithUnit("By"),
	)
	memtableZombieCount, _ := meter.Int64Gauge(
		"pebble_memtable_zombie_count",
		metric.WithDescription("Count of zombie memtables"),
		metric.WithUnit("{count}"),
	)

	walSize, _ := meter.Int64Gauge(
		"pebble_wal_size",
		metric.WithDescription("Current size of Write-Ahead Log"),
		metric.WithUnit("By"),
	)
	walFiles, _ := meter.Int64Gauge(
		"pebble_wal_files",
		metric.WithDescription("Number of live WAL files"),
		metric.WithUnit("{count}"),
	)
	walObsoleteFiles, _ := meter.Int64Gauge(
		"pebble_wal_obsolete_files",
		metric.WithDescription("Number of obsolete WAL files"),
		metric.WithUnit("{count}"),
	)
	walObsoletePhysicalSize, _ := meter.Int64Gauge(
		"pebble_wal_obsolete_physical_size",
		metric.WithDescription("Physical size of obsolete WAL files"),
		metric.WithUnit("By"),
	)
	walPhysicalSize, _ := meter.Int64Gauge(
		"pebble_wal_physical_size",
		metric.WithDescription("Physical size of WAL files on disk"),
		metric.WithUnit("By"),
	)
	walBytesIn, _ := meter.Int64Counter(
		"pebble_wal_bytes_in",
		metric.WithDescription("Logical bytes written to WAL"),
		metric.WithUnit("By"),
	)
	walBytesWritten, _ := meter.Int64Counter(
		"pebble_wal_bytes_written",
		metric.WithDescription("Bytes written to WAL"),
		metric.WithUnit("By"),
	)

	tableObsoleteSize, _ := meter.Int64Gauge(
		"pebble_table_obsolete_size",
		metric.WithDescription("Bytes in obsolete tables no longer referenced"),
		metric.WithUnit("By"),
	)
	tableObsoleteCount, _ := meter.Int64Gauge(
		"pebble_table_obsolete_count",
		metric.WithDescription("Count of obsolete tables"),
		metric.WithUnit("{count}"),
	)
	tableZombieSize, _ := meter.Int64Gauge(
		"pebble_table_zombie_size",
		metric.WithDescription("Bytes in zombie tables (released but in use by iterators)"),
		metric.WithUnit("By"),
	)
	tableZombieCount, _ := meter.Int64Gauge(
		"pebble_table_zombie_count",
		metric.WithDescription("Count of zombie tables"),
		metric.WithUnit("{count}"),
	)
	tableLiveSize, _ := meter.Int64Gauge(
		"pebble_table_live_size",
		metric.WithDescription("Bytes in live tables"),
		metric.WithUnit("By"),
	)
	tableLiveCount, _ := meter.Int64Gauge(
		"pebble_table_live_count",
		metric.WithDescription("Count of live tables"),
		metric.WithUnit("{count}"),
	)

	keysRangeKeySetsCount, _ := meter.Int64Gauge(
		"pebble_keys_range_key_sets_count",
		metric.WithDescription("Approximate count of internal range key set keys"),
		metric.WithUnit("{count}"),
	)
	keysTombstoneCount, _ := meter.Int64Gauge(
		"pebble_keys_tombstone_count",
		metric.WithDescription("Approximate count of internal tombstones"),
		metric.WithUnit("{count}"),
	)
	keysMissizedTombstonesCount, _ := meter.Int64Counter(
		"pebble_keys_missized_tombstones_count",
		metric.WithDescription("Missized DELSIZED keys encountered by compactions"),
		metric.WithUnit("{count}"),
	)

	snapshotCount, _ := meter.Int64Gauge(
		"pebble_snapshot_count",
		metric.WithDescription("Number of currently open snapshots"),
		metric.WithUnit("{count}"),
	)
	snapshotPinnedKeys, _ := meter.Int64Counter(
		"pebble_snapshot_pinned_keys",
		metric.WithDescription("Keys written that would've been elided without open snapshots"),
		metric.WithUnit("{count}"),
	)
	snapshotPinnedSize, _ := meter.Int64Counter(
		"pebble_snapshot_pinned_size",
		metric.WithDescription("Size of keys/values written due to open snapshots"),
		metric.WithUnit("By"),
	)

	tableIters, _ := meter.Int64Gauge(
		"pebble_table_iters",
		metric.WithDescription("Count of open sstable iterators"),
		metric.WithUnit("{count}"),
	)
	uptimeSeconds, _ := meter.Float64Gauge(
		"pebble_uptime_seconds",
		metric.WithDescription("Seconds since DB was opened"),
		metric.WithUnit("s"),
	)
	readAmp, _ := meter.Int64Gauge(
		"pebble_read_amp",
		metric.WithDescription("Read amplification"),
		metric.WithUnit("{count}"),
	)
	diskSpaceUsage, _ := meter.Int64Gauge(
		"pebble_disk_space_usage",
		metric.WithDescription("Total disk space used by the DB"),
		metric.WithUnit("By"),
	)

	cacheHits, _ := meter.Int64Counter(
		"pebble_cache_hits",
		metric.WithDescription("Total number of cache hits"),
		metric.WithUnit("{count}"),
	)
	cacheMisses, _ := meter.Int64Counter(
		"pebble_cache_misses",
		metric.WithDescription("Total number of cache misses"),
		metric.WithUnit("{count}"),
	)
	cacheSize, _ := meter.Int64Gauge(
		"pebble_cache_size",
		metric.WithDescription("Current cache size"),
		metric.WithUnit("By"),
	)

	batchSize, _ := meter.Int64Histogram(
		"pebble_batch_size",
		metric.WithDescription("Size of batches written to PebbleDB"),
		metric.WithUnit("By"),
	)
	pendingChangesQueueDepth, _ := meter.Int64Gauge(
		"pebble_pending_changes_queue_depth",
		metric.WithDescription("Number of pending changesets in async write queue"),
		metric.WithUnit("{count}"),
	)
	iteratorIterations, _ := meter.Float64Histogram(
		"pebble_iterator_iterations",
		metric.WithDescription("Number of iterations per iterator"),
		metric.WithUnit("{count}"),
	)

	pm := &PebbleMetrics{
		db: db, databaseName: databaseName,

		getLatency:                 getLatency,
		applyChangesetLatency:      applyChangesetLatency,
		applyChangesetAsyncLatency: applyChangesetAsyncLatency,
		pruneLatency:               pruneLatency,
		importLatency:              importLatency,
		batchWriteLatency:          batchWriteLatency,

		compactionCount:           compactionCount,
		compactionDuration:        compactionDuration,
		compactionBytesRead:       compactionBytesRead,
		compactionBytesWritten:    compactionBytesWritten,
		compactionEstimatedDebt:   compactionEstimatedDebt,
		compactionInProgressBytes: compactionInProgressBytes,
		compactionNumInProgress:   compactionNumInProgress,
		compactionCancelledCount:  compactionCancelledCount,
		compactionCancelledBytes:  compactionCancelledBytes,
		compactionFailedCount:     compactionFailedCount,

		ingestCount: ingestCount,

		flushCount:              flushCount,
		flushDuration:           flushDuration,
		flushBytesWritten:       flushBytesWritten,
		flushNumInProgress:      flushNumInProgress,
		flushAsIngestCount:      flushAsIngestCount,
		flushAsIngestTableCount: flushAsIngestTableCount,
		flushAsIngestBytes:      flushAsIngestBytes,

		sstableCount:           sstableCount,
		sstableTotalSize:       sstableTotalSize,
		sstableSublevels:       sstableSublevels,
		sstableScore:           sstableScore,
		sstableFillFactor:      sstableFillFactor,
		sstableVirtualCount:    sstableVirtualCount,
		sstableVirtualSize:     sstableVirtualSize,
		sstableBytesIngested:   sstableBytesIngested,
		sstableBytesMoved:      sstableBytesMoved,
		sstableBytesRead:       sstableBytesRead,
		sstableBytesFlushed:    sstableBytesFlushed,
		sstableTablesCompacted: sstableTablesCompacted,
		sstableTablesFlushed:   sstableTablesFlushed,
		sstableTablesIngested:  sstableTablesIngested,
		sstableTablesMoved:     sstableTablesMoved,

		memtableCount:       memtableCount,
		memtableTotalSize:   memtableTotalSize,
		memtableZombieSize:  memtableZombieSize,
		memtableZombieCount: memtableZombieCount,

		walSize:                 walSize,
		walFiles:                walFiles,
		walObsoleteFiles:        walObsoleteFiles,
		walObsoletePhysicalSize: walObsoletePhysicalSize,
		walPhysicalSize:         walPhysicalSize,
		walBytesIn:              walBytesIn,
		walBytesWritten:         walBytesWritten,

		tableObsoleteSize:  tableObsoleteSize,
		tableObsoleteCount: tableObsoleteCount,
		tableZombieSize:    tableZombieSize,
		tableZombieCount:   tableZombieCount,
		tableLiveSize:      tableLiveSize,
		tableLiveCount:     tableLiveCount,

		keysRangeKeySetsCount:       keysRangeKeySetsCount,
		keysTombstoneCount:          keysTombstoneCount,
		keysMissizedTombstonesCount: keysMissizedTombstonesCount,

		snapshotCount:      snapshotCount,
		snapshotPinnedKeys: snapshotPinnedKeys,
		snapshotPinnedSize: snapshotPinnedSize,

		tableIters:     tableIters,
		uptimeSeconds:  uptimeSeconds,
		readAmp:        readAmp,
		diskSpaceUsage: diskSpaceUsage,

		cacheHits:   cacheHits,
		cacheMisses: cacheMisses,
		cacheSize:   cacheSize,

		batchSize:                batchSize,
		pendingChangesQueueDepth: pendingChangesQueueDepth,
		iteratorIterations:       iteratorIterations,
	}

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

func uint64ToInt64Clamped(v uint64) int64 {
	if v > math.MaxInt64 {
		return math.MaxInt64
	}
	return int64(v)
}

// recordFromPebble fetches the current metrics from the Pebble DB via Metrics(), then
// records compaction, flush, level, memtable, WAL, and cache metrics with the configured
// database name as the "db" attribute.
func (pm *PebbleMetrics) recordFromPebble(ctx context.Context) {
	if pm.db == nil {
		return
	}
	m := pm.db.Metrics()
	dbAttr := attribute.String("db", pm.databaseName)

	if pm.compactionCount != nil {
		pm.compactionCount.Add(ctx, m.Compact.Count, metric.WithAttributes(dbAttr))
	}
	if pm.compactionDuration != nil {
		pm.compactionDuration.Record(ctx, m.Compact.Duration.Seconds(), metric.WithAttributes(dbAttr))
	}
	if pm.compactionEstimatedDebt != nil {
		pm.compactionEstimatedDebt.Record(ctx,
			uint64ToInt64Clamped(m.Compact.EstimatedDebt), metric.WithAttributes(dbAttr))
	}
	if pm.compactionInProgressBytes != nil {
		pm.compactionInProgressBytes.Record(ctx, m.Compact.InProgressBytes, metric.WithAttributes(dbAttr))
	}
	if pm.compactionNumInProgress != nil {
		pm.compactionNumInProgress.Record(ctx, m.Compact.NumInProgress, metric.WithAttributes(dbAttr))
	}
	if pm.compactionCancelledCount != nil {
		pm.compactionCancelledCount.Add(ctx, m.Compact.CancelledCount, metric.WithAttributes(dbAttr))
	}
	if pm.compactionCancelledBytes != nil {
		pm.compactionCancelledBytes.Add(ctx, m.Compact.CancelledBytes, metric.WithAttributes(dbAttr))
	}
	if pm.compactionFailedCount != nil {
		pm.compactionFailedCount.Add(ctx, m.Compact.FailedCount, metric.WithAttributes(dbAttr))
	}

	if pm.ingestCount != nil {
		pm.ingestCount.Add(ctx, uint64ToInt64Clamped(m.Ingest.Count), metric.WithAttributes(dbAttr))
	}

	if pm.flushCount != nil {
		pm.flushCount.Add(ctx, m.Flush.Count, metric.WithAttributes(dbAttr))
	}
	if pm.flushDuration != nil {
		pm.flushDuration.Record(ctx,
			m.Flush.WriteThroughput.WorkDuration.Seconds(), metric.WithAttributes(dbAttr))
	}
	if pm.flushBytesWritten != nil {
		pm.flushBytesWritten.Add(ctx, m.Flush.WriteThroughput.Bytes, metric.WithAttributes(dbAttr))
	}
	if pm.flushNumInProgress != nil {
		pm.flushNumInProgress.Record(ctx, m.Flush.NumInProgress, metric.WithAttributes(dbAttr))
	}
	if pm.flushAsIngestCount != nil {
		pm.flushAsIngestCount.Add(ctx, uint64ToInt64Clamped(m.Flush.AsIngestCount), metric.WithAttributes(dbAttr))
	}
	if pm.flushAsIngestTableCount != nil {
		pm.flushAsIngestTableCount.Add(ctx, uint64ToInt64Clamped(m.Flush.AsIngestTableCount),
			metric.WithAttributes(dbAttr))
	}
	if pm.flushAsIngestBytes != nil {
		pm.flushAsIngestBytes.Add(ctx,
			uint64ToInt64Clamped(m.Flush.AsIngestBytes), metric.WithAttributes(dbAttr))
	}

	for level := 0; level < len(m.Levels); level++ {
		lm := m.Levels[level]
		levelAttr := attribute.Int("level", level)
		attrs := metric.WithAttributes(dbAttr, levelAttr)

		if pm.sstableCount != nil {
			pm.sstableCount.Record(ctx, lm.TablesCount, attrs)
		}
		if pm.sstableTotalSize != nil {
			pm.sstableTotalSize.Record(ctx, lm.TablesSize, attrs)
		}
		if pm.sstableSublevels != nil {
			pm.sstableSublevels.Record(ctx, int64(lm.Sublevels), attrs)
		}
		if pm.sstableScore != nil {
			pm.sstableScore.Record(ctx, lm.Score, attrs)
		}
		if pm.sstableFillFactor != nil {
			pm.sstableFillFactor.Record(ctx, lm.FillFactor, attrs)
		}
		if pm.sstableVirtualCount != nil {
			pm.sstableVirtualCount.Record(ctx, uint64ToInt64Clamped(lm.VirtualTablesCount), attrs)
		}
		if pm.sstableVirtualSize != nil {
			pm.sstableVirtualSize.Record(ctx, uint64ToInt64Clamped(lm.VirtualTablesSize), attrs)
		}
		if pm.compactionBytesRead != nil {
			pm.compactionBytesRead.Add(ctx, uint64ToInt64Clamped(lm.TableBytesIn), attrs)
		}
		if pm.compactionBytesWritten != nil {
			pm.compactionBytesWritten.Add(ctx, uint64ToInt64Clamped(lm.TableBytesCompacted), attrs)
		}
		if pm.sstableBytesIngested != nil {
			pm.sstableBytesIngested.Add(ctx, uint64ToInt64Clamped(lm.TableBytesIngested), attrs)
		}
		if pm.sstableBytesMoved != nil {
			pm.sstableBytesMoved.Add(ctx, uint64ToInt64Clamped(lm.TableBytesMoved), attrs)
		}
		if pm.sstableBytesRead != nil {
			pm.sstableBytesRead.Add(ctx, uint64ToInt64Clamped(lm.TableBytesRead), attrs)
		}
		if pm.sstableBytesFlushed != nil {
			pm.sstableBytesFlushed.Add(ctx, uint64ToInt64Clamped(lm.TableBytesFlushed), attrs)
		}
		if pm.sstableTablesCompacted != nil {
			pm.sstableTablesCompacted.Add(ctx, uint64ToInt64Clamped(lm.TablesCompacted), attrs)
		}
		if pm.sstableTablesFlushed != nil {
			pm.sstableTablesFlushed.Add(ctx, uint64ToInt64Clamped(lm.TablesFlushed), attrs)
		}
		if pm.sstableTablesIngested != nil {
			pm.sstableTablesIngested.Add(ctx, uint64ToInt64Clamped(lm.TablesIngested), attrs)
		}
		if pm.sstableTablesMoved != nil {
			pm.sstableTablesMoved.Add(ctx, uint64ToInt64Clamped(lm.TablesMoved), attrs)
		}
	}

	if pm.memtableCount != nil {
		pm.memtableCount.Record(ctx, m.MemTable.Count, metric.WithAttributes(dbAttr))
	}
	if pm.memtableTotalSize != nil {
		pm.memtableTotalSize.Record(ctx, uint64ToInt64Clamped(m.MemTable.Size), metric.WithAttributes(dbAttr))
	}
	if pm.memtableZombieSize != nil {
		pm.memtableZombieSize.Record(ctx,
			uint64ToInt64Clamped(m.MemTable.ZombieSize), metric.WithAttributes(dbAttr))
	}
	if pm.memtableZombieCount != nil {
		pm.memtableZombieCount.Record(ctx, m.MemTable.ZombieCount, metric.WithAttributes(dbAttr))
	}

	if pm.walSize != nil {
		pm.walSize.Record(ctx, uint64ToInt64Clamped(m.WAL.Size), metric.WithAttributes(dbAttr))
	}
	if pm.walFiles != nil {
		pm.walFiles.Record(ctx, m.WAL.Files, metric.WithAttributes(dbAttr))
	}
	if pm.walObsoleteFiles != nil {
		pm.walObsoleteFiles.Record(ctx, m.WAL.ObsoleteFiles, metric.WithAttributes(dbAttr))
	}
	if pm.walObsoletePhysicalSize != nil {
		pm.walObsoletePhysicalSize.Record(ctx,
			uint64ToInt64Clamped(m.WAL.ObsoletePhysicalSize), metric.WithAttributes(dbAttr))
	}
	if pm.walPhysicalSize != nil {
		pm.walPhysicalSize.Record(ctx, uint64ToInt64Clamped(m.WAL.PhysicalSize), metric.WithAttributes(dbAttr))
	}
	if pm.walBytesIn != nil {
		pm.walBytesIn.Add(ctx, uint64ToInt64Clamped(m.WAL.BytesIn), metric.WithAttributes(dbAttr))
	}
	if pm.walBytesWritten != nil {
		pm.walBytesWritten.Add(ctx, uint64ToInt64Clamped(m.WAL.BytesWritten), metric.WithAttributes(dbAttr))
	}

	if pm.tableObsoleteSize != nil {
		pm.tableObsoleteSize.Record(ctx,
			uint64ToInt64Clamped(m.Table.ObsoleteSize), metric.WithAttributes(dbAttr))
	}
	if pm.tableObsoleteCount != nil {
		pm.tableObsoleteCount.Record(ctx, m.Table.ObsoleteCount, metric.WithAttributes(dbAttr))
	}
	if pm.tableZombieSize != nil {
		pm.tableZombieSize.Record(ctx,
			uint64ToInt64Clamped(m.Table.ZombieSize), metric.WithAttributes(dbAttr))
	}
	if pm.tableZombieCount != nil {
		pm.tableZombieCount.Record(ctx, m.Table.ZombieCount, metric.WithAttributes(dbAttr))
	}
	if pm.tableLiveSize != nil {
		pm.tableLiveSize.Record(ctx,
			uint64ToInt64Clamped(m.Table.Local.LiveSize), metric.WithAttributes(dbAttr))
	}
	if pm.tableLiveCount != nil {
		pm.tableLiveCount.Record(ctx,
			uint64ToInt64Clamped(m.Table.Local.LiveCount), metric.WithAttributes(dbAttr))
	}

	if pm.keysRangeKeySetsCount != nil {
		pm.keysRangeKeySetsCount.Record(ctx,
			uint64ToInt64Clamped(m.Keys.RangeKeySetsCount), metric.WithAttributes(dbAttr))
	}
	if pm.keysTombstoneCount != nil {
		pm.keysTombstoneCount.Record(ctx,
			uint64ToInt64Clamped(m.Keys.TombstoneCount), metric.WithAttributes(dbAttr))
	}
	if pm.keysMissizedTombstonesCount != nil {
		pm.keysMissizedTombstonesCount.Add(ctx,
			uint64ToInt64Clamped(m.Keys.MissizedTombstonesCount), metric.WithAttributes(dbAttr))
	}

	if pm.snapshotCount != nil {
		pm.snapshotCount.Record(ctx, int64(m.Snapshots.Count), metric.WithAttributes(dbAttr))
	}
	if pm.snapshotPinnedKeys != nil {
		pm.snapshotPinnedKeys.Add(ctx,
			uint64ToInt64Clamped(m.Snapshots.PinnedKeys), metric.WithAttributes(dbAttr))
	}
	if pm.snapshotPinnedSize != nil {
		pm.snapshotPinnedSize.Add(ctx,
			uint64ToInt64Clamped(m.Snapshots.PinnedSize), metric.WithAttributes(dbAttr))
	}

	if pm.tableIters != nil {
		pm.tableIters.Record(ctx, m.TableIters, metric.WithAttributes(dbAttr))
	}
	if pm.uptimeSeconds != nil {
		pm.uptimeSeconds.Record(ctx, m.Uptime.Seconds(), metric.WithAttributes(dbAttr))
	}
	if pm.readAmp != nil {
		pm.readAmp.Record(ctx, int64(m.ReadAmp()), metric.WithAttributes(dbAttr))
	}
	if pm.diskSpaceUsage != nil {
		pm.diskSpaceUsage.Record(ctx, uint64ToInt64Clamped(m.DiskSpaceUsage()), metric.WithAttributes(dbAttr))
	}

	if pm.cacheHits != nil {
		pm.cacheHits.Add(ctx, m.BlockCache.Hits, metric.WithAttributes(dbAttr))
	}
	if pm.cacheMisses != nil {
		pm.cacheMisses.Add(ctx, m.BlockCache.Misses, metric.WithAttributes(dbAttr))
	}
	if pm.cacheSize != nil {
		pm.cacheSize.Record(ctx, m.BlockCache.Size, metric.WithAttributes(dbAttr))
	}
}
