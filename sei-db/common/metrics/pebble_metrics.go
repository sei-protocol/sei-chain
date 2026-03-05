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

	compactionCount                 metric.Int64Counter
	compactionDuration              metric.Float64Histogram
	compactionBytesRead             metric.Int64Counter
	compactionBytesWritten          metric.Int64Counter
	compactionEstimatedDebt         metric.Int64Gauge
	compactionInProgressBytes       metric.Int64Gauge
	compactionNumInProgress         metric.Int64Gauge
	compactionCancelledCount        metric.Int64Counter
	compactionCancelledBytes        metric.Int64Counter
	compactionFailedCount           metric.Int64Counter
	compactionDefaultCount          metric.Int64Counter
	compactionDeleteOnlyCount       metric.Int64Counter
	compactionElisionOnlyCount      metric.Int64Counter
	compactionCopyCount             metric.Int64Counter
	compactionMoveCount             metric.Int64Counter
	compactionReadCount             metric.Int64Counter
	compactionTombstoneDensityCount metric.Int64Counter
	compactionRewriteCount          metric.Int64Counter
	compactionMultiLevelCount       metric.Int64Counter
	compactionBlobFileRewriteCount  metric.Int64Counter
	compactionCounterLevelCount     metric.Int64Counter
	compactionNumProblemSpans       metric.Int64Gauge
	compactionMarkedFiles           metric.Int64Gauge

	ingestCount metric.Int64Counter

	flushCount              metric.Int64Counter
	flushDuration           metric.Float64Histogram
	flushBytesWritten       metric.Int64Counter
	flushNumInProgress      metric.Int64Gauge
	flushAsIngestCount      metric.Int64Counter
	flushAsIngestTableCount metric.Int64Counter
	flushAsIngestBytes      metric.Int64Counter
	flushIdleDuration       metric.Float64Gauge

	filterHits   metric.Int64Counter
	filterMisses metric.Int64Counter

	sstableCount                   metric.Int64Gauge
	sstableTotalSize               metric.Int64Gauge
	sstableSublevels               metric.Int64Gauge
	sstableScore                   metric.Float64Gauge
	sstableFillFactor              metric.Float64Gauge
	sstableVirtualCount            metric.Int64Gauge
	sstableVirtualSize             metric.Int64Gauge
	sstableBytesIngested           metric.Int64Counter
	sstableBytesMoved              metric.Int64Counter
	sstableBytesRead               metric.Int64Counter
	sstableBytesFlushed            metric.Int64Counter
	sstableTablesCompacted         metric.Int64Counter
	sstableTablesFlushed           metric.Int64Counter
	sstableTablesIngested          metric.Int64Counter
	sstableTablesMoved             metric.Int64Counter
	sstableCompensatedFillFactor   metric.Float64Gauge
	sstableEstimatedReferencesSize metric.Int64Gauge
	sstableTablesDeleted           metric.Int64Counter
	sstableTablesExcised           metric.Int64Counter
	sstableBlobBytesReadEstimate   metric.Int64Counter
	sstableBlobBytesCompacted      metric.Int64Counter
	sstableBlobBytesFlushed        metric.Int64Counter
	sstableMultiLevelBytesInTop    metric.Int64Counter
	sstableMultiLevelBytesIn       metric.Int64Counter
	sstableMultiLevelBytesRead     metric.Int64Counter
	sstableValueBlocksSize         metric.Int64Gauge
	sstableBytesWrittenDataBlocks  metric.Int64Counter
	sstableBytesWrittenValueBlocks metric.Int64Counter

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

	tableObsoleteSize                  metric.Int64Gauge
	tableObsoleteCount                 metric.Int64Gauge
	tableZombieSize                    metric.Int64Gauge
	tableZombieCount                   metric.Int64Gauge
	tableLiveSize                      metric.Int64Gauge
	tableLiveCount                     metric.Int64Gauge
	tableBackingCount                  metric.Int64Gauge
	tableBackingSize                   metric.Int64Gauge
	tableCompressedUnknown             metric.Int64Gauge
	tableCompressedSnappy              metric.Int64Gauge
	tableCompressedZstd                metric.Int64Gauge
	tableCompressedMinLZ               metric.Int64Gauge
	tableCompressedNone                metric.Int64Gauge
	tableLocalObsoleteSize             metric.Int64Gauge
	tableLocalObsoleteCount            metric.Int64Gauge
	tableLocalZombieSize               metric.Int64Gauge
	tableLocalZombieCount              metric.Int64Gauge
	tableGarbagePointDeletionsEstimate metric.Int64Gauge
	tableGarbageRangeDeletionsEstimate metric.Int64Gauge
	tableInitialStatsComplete          metric.Int64Gauge
	tablePendingStatsCount             metric.Int64Gauge

	blobFilesLiveCount           metric.Int64Gauge
	blobFilesLiveSize            metric.Int64Gauge
	blobFilesValueSize           metric.Int64Gauge
	blobFilesReferencedValueSize metric.Int64Gauge
	blobFilesObsoleteCount       metric.Int64Gauge
	blobFilesObsoleteSize        metric.Int64Gauge
	blobFilesZombieCount         metric.Int64Gauge
	blobFilesZombieSize          metric.Int64Gauge
	blobFilesLocalLiveSize       metric.Int64Gauge
	blobFilesLocalLiveCount      metric.Int64Gauge
	blobFilesLocalObsoleteSize   metric.Int64Gauge
	blobFilesLocalObsoleteCount  metric.Int64Gauge
	blobFilesLocalZombieSize     metric.Int64Gauge
	blobFilesLocalZombieCount    metric.Int64Gauge

	fileCacheSize          metric.Int64Gauge
	fileCacheTableCount    metric.Int64Gauge
	fileCacheBlobFileCount metric.Int64Gauge
	fileCacheHits          metric.Int64Counter
	fileCacheMisses        metric.Int64Counter

	// prev* track last scraped cumulative values so we Add(delta) not Add(total).
	prevCompactionCount                 int64
	prevCompactionCancelledCount        int64
	prevCompactionCancelledBytes        int64
	prevCompactionFailedCount           int64
	prevCompactionDefaultCount          int64
	prevCompactionDeleteOnlyCount       int64
	prevCompactionElisionOnlyCount      int64
	prevCompactionCopyCount             int64
	prevCompactionMoveCount             int64
	prevCompactionReadCount             int64
	prevCompactionTombstoneDensityCount int64
	prevCompactionRewriteCount          int64
	prevCompactionMultiLevelCount       int64
	prevCompactionBlobFileRewriteCount  int64
	prevCompactionCounterLevelCount     int64
	prevIngestCount                     int64
	prevFlushCount                      int64
	prevFlushBytesWritten               int64
	prevFlushAsIngestCount              int64
	prevFlushAsIngestTableCount         int64
	prevFlushAsIngestBytes              int64
	prevFilterHits                      int64
	prevFilterMisses                    int64
	prevWalBytesIn                      int64
	prevWalBytesWritten                 int64
	prevWalFailoverDirSwitchCount       int64
	prevKeysMissizedTombstonesCount     int64
	prevSnapshotPinnedKeys              int64
	prevSnapshotPinnedSize              int64
	prevFileCacheHits                   int64
	prevFileCacheMisses                 int64
	prevCacheHits                       int64
	prevCacheMisses                     int64

	// prev*ByLevel hold previous cumulative values per level (index = level).
	prevCompactionBytesReadByLevel            []int64
	prevCompactionBytesWrittenByLevel         []int64
	prevSstableBytesIngestedByLevel           []int64
	prevSstableBytesMovedByLevel              []int64
	prevSstableBytesReadByLevel               []int64
	prevSstableBytesFlushedByLevel            []int64
	prevSstableTablesCompactedByLevel         []int64
	prevSstableTablesFlushedByLevel           []int64
	prevSstableTablesIngestedByLevel          []int64
	prevSstableTablesMovedByLevel             []int64
	prevSstableTablesDeletedByLevel           []int64
	prevSstableTablesExcisedByLevel           []int64
	prevSstableBlobBytesReadEstimateByLevel   []int64
	prevSstableBlobBytesCompactedByLevel      []int64
	prevSstableBlobBytesFlushedByLevel        []int64
	prevSstableMultiLevelBytesInTopByLevel    []int64
	prevSstableMultiLevelBytesInByLevel       []int64
	prevSstableMultiLevelBytesReadByLevel     []int64
	prevSstableBytesWrittenDataBlocksByLevel  []int64
	prevSstableBytesWrittenValueBlocksByLevel []int64

	walFailoverDirSwitchCount    metric.Int64Counter
	walFailoverPrimaryDuration   metric.Float64Gauge
	walFailoverSecondaryDuration metric.Float64Gauge

	numVirtual        metric.Int64Gauge
	virtualSize       metric.Int64Gauge
	remoteTablesCount metric.Int64Gauge
	remoteTablesSize  metric.Int64Gauge

	keysRangeKeySetsCount       metric.Int64Gauge
	keysTombstoneCount          metric.Int64Gauge
	keysMissizedTombstonesCount metric.Int64Counter

	snapshotCount          metric.Int64Gauge
	snapshotPinnedKeys     metric.Int64Counter
	snapshotPinnedSize     metric.Int64Counter
	snapshotEarliestSeqNum metric.Int64Gauge

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
	compactionDefaultCount, _ := meter.Int64Counter(
		"pebble_compaction_default_count",
		metric.WithDescription("Default compactions"),
		metric.WithUnit("{count}"),
	)
	compactionDeleteOnlyCount, _ := meter.Int64Counter(
		"pebble_compaction_delete_only_count",
		metric.WithDescription("Delete-only compactions"),
		metric.WithUnit("{count}"),
	)
	compactionElisionOnlyCount, _ := meter.Int64Counter(
		"pebble_compaction_elision_only_count",
		metric.WithDescription("Elision-only compactions"),
		metric.WithUnit("{count}"),
	)
	compactionCopyCount, _ := meter.Int64Counter(
		"pebble_compaction_copy_count",
		metric.WithDescription("Copy compactions"),
		metric.WithUnit("{count}"),
	)
	compactionMoveCount, _ := meter.Int64Counter(
		"pebble_compaction_move_count",
		metric.WithDescription("Move compactions"),
		metric.WithUnit("{count}"),
	)
	compactionReadCount, _ := meter.Int64Counter(
		"pebble_compaction_read_count",
		metric.WithDescription("Read compactions"),
		metric.WithUnit("{count}"),
	)
	compactionTombstoneDensityCount, _ := meter.Int64Counter(
		"pebble_compaction_tombstone_density_count",
		metric.WithDescription("Tombstone-density compactions"),
		metric.WithUnit("{count}"),
	)
	compactionRewriteCount, _ := meter.Int64Counter(
		"pebble_compaction_rewrite_count",
		metric.WithDescription("Rewrite compactions"),
		metric.WithUnit("{count}"),
	)
	compactionMultiLevelCount, _ := meter.Int64Counter(
		"pebble_compaction_multi_level_count",
		metric.WithDescription("Multi-level compactions"),
		metric.WithUnit("{count}"),
	)
	compactionBlobFileRewriteCount, _ := meter.Int64Counter(
		"pebble_compaction_blob_file_rewrite_count",
		metric.WithDescription("Blob file rewrite compactions"),
		metric.WithUnit("{count}"),
	)
	compactionCounterLevelCount, _ := meter.Int64Counter(
		"pebble_compaction_counter_level_count",
		metric.WithDescription("Counter-level compactions"),
		metric.WithUnit("{count}"),
	)
	compactionNumProblemSpans, _ := meter.Int64Gauge(
		"pebble_compaction_num_problem_spans",
		metric.WithDescription("Problem spans blocking compactions"),
		metric.WithUnit("{count}"),
	)
	compactionMarkedFiles, _ := meter.Int64Gauge(
		"pebble_compaction_marked_files",
		metric.WithDescription("Files marked for compaction"),
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
	flushIdleDuration, _ := meter.Float64Gauge(
		"pebble_flush_idle_duration",
		metric.WithDescription("Idle duration before memtable flushes"),
		metric.WithUnit("s"),
	)

	filterHits, _ := meter.Int64Counter(
		"pebble_filter_hits",
		metric.WithDescription("Bloom filter hits (block reads avoided)"),
		metric.WithUnit("{count}"),
	)
	filterMisses, _ := meter.Int64Counter(
		"pebble_filter_misses",
		metric.WithDescription("Bloom filter misses"),
		metric.WithUnit("{count}"),
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
	sstableCompensatedFillFactor, _ := meter.Float64Gauge(
		"pebble_sstable_compensated_fill_factor",
		metric.WithDescription("Level compensated fill factor"),
		metric.WithUnit("1"),
	)
	sstableEstimatedReferencesSize, _ := meter.Int64Gauge(
		"pebble_sstable_estimated_references_size",
		metric.WithDescription("Est. physical size of blob refs at level"),
		metric.WithUnit("By"),
	)
	sstableTablesDeleted, _ := meter.Int64Counter(
		"pebble_sstable_tables_deleted",
		metric.WithDescription("Sstables deleted by delete-only compaction at level"),
		metric.WithUnit("{count}"),
	)
	sstableTablesExcised, _ := meter.Int64Counter(
		"pebble_sstable_tables_excised",
		metric.WithDescription("Sstables excised by delete-only compaction at level"),
		metric.WithUnit("{count}"),
	)
	sstableBlobBytesReadEstimate, _ := meter.Int64Counter(
		"pebble_sstable_blob_bytes_read_estimate",
		metric.WithDescription("Est. physical bytes read for blob refs at level"),
		metric.WithUnit("By"),
	)
	sstableBlobBytesCompacted, _ := meter.Int64Counter(
		"pebble_sstable_blob_bytes_compacted",
		metric.WithDescription("Blob bytes written during compaction at level"),
		metric.WithUnit("By"),
	)
	sstableBlobBytesFlushed, _ := meter.Int64Counter(
		"pebble_sstable_blob_bytes_flushed",
		metric.WithDescription("Blob bytes written during flush at level"),
		metric.WithUnit("By"),
	)
	sstableMultiLevelBytesInTop, _ := meter.Int64Counter(
		"pebble_sstable_multi_level_bytes_in_top",
		metric.WithDescription("Bytes from top level in multilevel compaction"),
		metric.WithUnit("By"),
	)
	sstableMultiLevelBytesIn, _ := meter.Int64Counter(
		"pebble_sstable_multi_level_bytes_in",
		metric.WithDescription("Bytes in for multilevel compaction"),
		metric.WithUnit("By"),
	)
	sstableMultiLevelBytesRead, _ := meter.Int64Counter(
		"pebble_sstable_multi_level_bytes_read",
		metric.WithDescription("Bytes read for multilevel compaction"),
		metric.WithUnit("By"),
	)
	sstableValueBlocksSize, _ := meter.Int64Gauge(
		"pebble_sstable_value_blocks_size",
		metric.WithDescription("Value blocks size at level"),
		metric.WithUnit("By"),
	)
	sstableBytesWrittenDataBlocks, _ := meter.Int64Counter(
		"pebble_sstable_bytes_written_data_blocks",
		metric.WithDescription("Bytes written to data blocks at level"),
		metric.WithUnit("By"),
	)
	sstableBytesWrittenValueBlocks, _ := meter.Int64Counter(
		"pebble_sstable_bytes_written_value_blocks",
		metric.WithDescription("Bytes written to value blocks at level"),
		metric.WithUnit("By"),
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
	tableBackingCount, _ := meter.Int64Gauge(
		"pebble_table_backing_count",
		metric.WithDescription("Sstables backing virtual tables"),
		metric.WithUnit("{count}"),
	)
	tableBackingSize, _ := meter.Int64Gauge(
		"pebble_table_backing_size",
		metric.WithDescription("Size of sstables backing virtual tables"),
		metric.WithUnit("By"),
	)
	tableCompressedUnknown, _ := meter.Int64Gauge(
		"pebble_table_compressed_unknown",
		metric.WithDescription("Sstables with unknown compression"),
		metric.WithUnit("{count}"),
	)
	tableCompressedSnappy, _ := meter.Int64Gauge(
		"pebble_table_compressed_snappy",
		metric.WithDescription("Snappy-compressed sstables"),
		metric.WithUnit("{count}"),
	)
	tableCompressedZstd, _ := meter.Int64Gauge(
		"pebble_table_compressed_zstd",
		metric.WithDescription("Zstd-compressed sstables"),
		metric.WithUnit("{count}"),
	)
	tableCompressedMinLZ, _ := meter.Int64Gauge(
		"pebble_table_compressed_minlz",
		metric.WithDescription("MinLZ-compressed sstables"),
		metric.WithUnit("{count}"),
	)
	tableCompressedNone, _ := meter.Int64Gauge(
		"pebble_table_compressed_none",
		metric.WithDescription("Uncompressed sstables"),
		metric.WithUnit("{count}"),
	)
	tableLocalObsoleteSize, _ := meter.Int64Gauge(
		"pebble_table_local_obsolete_size",
		metric.WithDescription("Local obsolete table size"),
		metric.WithUnit("By"),
	)
	tableLocalObsoleteCount, _ := meter.Int64Gauge(
		"pebble_table_local_obsolete_count",
		metric.WithDescription("Local obsolete table count"),
		metric.WithUnit("{count}"),
	)
	tableLocalZombieSize, _ := meter.Int64Gauge(
		"pebble_table_local_zombie_size",
		metric.WithDescription("Local zombie table size"),
		metric.WithUnit("By"),
	)
	tableLocalZombieCount, _ := meter.Int64Gauge(
		"pebble_table_local_zombie_count",
		metric.WithDescription("Local zombie table count"),
		metric.WithUnit("{count}"),
	)
	tableGarbagePointDeletionsEstimate, _ := meter.Int64Gauge(
		"pebble_table_garbage_point_deletions_estimate",
		metric.WithDescription("Est. bytes reclaimable from point deletes"),
		metric.WithUnit("By"),
	)
	tableGarbageRangeDeletionsEstimate, _ := meter.Int64Gauge(
		"pebble_table_garbage_range_deletions_estimate",
		metric.WithDescription("Est. bytes reclaimable from range deletes"),
		metric.WithUnit("By"),
	)
	tableInitialStatsComplete, _ := meter.Int64Gauge(
		"pebble_table_initial_stats_complete",
		metric.WithDescription("1 if initial stats collection complete"),
		metric.WithUnit("1"),
	)
	tablePendingStatsCount, _ := meter.Int64Gauge(
		"pebble_table_pending_stats_count",
		metric.WithDescription("New sstables awaiting stats collection"),
		metric.WithUnit("{count}"),
	)
	blobFilesLiveCount, _ := meter.Int64Gauge(
		"pebble_blob_files_live_count",
		metric.WithDescription("Live blob file count"),
		metric.WithUnit("{count}"),
	)
	blobFilesLiveSize, _ := meter.Int64Gauge(
		"pebble_blob_files_live_size",
		metric.WithDescription("Live blob file physical size"),
		metric.WithUnit("By"),
	)
	blobFilesValueSize, _ := meter.Int64Gauge(
		"pebble_blob_files_value_size",
		metric.WithDescription("Uncompressed value size in live blobs"),
		metric.WithUnit("By"),
	)
	blobFilesReferencedValueSize, _ := meter.Int64Gauge(
		"pebble_blob_files_referenced_value_size",
		metric.WithDescription("Referenced value size in live blobs"),
		metric.WithUnit("By"),
	)
	blobFilesObsoleteCount, _ := meter.Int64Gauge(
		"pebble_blob_files_obsolete_count",
		metric.WithDescription("Obsolete blob file count"),
		metric.WithUnit("{count}"),
	)
	blobFilesObsoleteSize, _ := meter.Int64Gauge(
		"pebble_blob_files_obsolete_size",
		metric.WithDescription("Obsolete blob file size"),
		metric.WithUnit("By"),
	)
	blobFilesZombieCount, _ := meter.Int64Gauge(
		"pebble_blob_files_zombie_count",
		metric.WithDescription("Zombie blob file count"),
		metric.WithUnit("{count}"),
	)
	blobFilesZombieSize, _ := meter.Int64Gauge(
		"pebble_blob_files_zombie_size",
		metric.WithDescription("Zombie blob file size"),
		metric.WithUnit("By"),
	)
	blobFilesLocalLiveSize, _ := meter.Int64Gauge(
		"pebble_blob_files_local_live_size",
		metric.WithDescription("Local live blob file size"),
		metric.WithUnit("By"),
	)
	blobFilesLocalLiveCount, _ := meter.Int64Gauge(
		"pebble_blob_files_local_live_count",
		metric.WithDescription("Local live blob file count"),
		metric.WithUnit("{count}"),
	)
	blobFilesLocalObsoleteSize, _ := meter.Int64Gauge(
		"pebble_blob_files_local_obsolete_size",
		metric.WithDescription("Local obsolete blob file size"),
		metric.WithUnit("By"),
	)
	blobFilesLocalObsoleteCount, _ := meter.Int64Gauge(
		"pebble_blob_files_local_obsolete_count",
		metric.WithDescription("Local obsolete blob file count"),
		metric.WithUnit("{count}"),
	)
	blobFilesLocalZombieSize, _ := meter.Int64Gauge(
		"pebble_blob_files_local_zombie_size",
		metric.WithDescription("Local zombie blob file size"),
		metric.WithUnit("By"),
	)
	blobFilesLocalZombieCount, _ := meter.Int64Gauge(
		"pebble_blob_files_local_zombie_count",
		metric.WithDescription("Local zombie blob file count"),
		metric.WithUnit("{count}"),
	)
	fileCacheSize, _ := meter.Int64Gauge(
		"pebble_file_cache_size",
		metric.WithDescription("Bytes in file cache"),
		metric.WithUnit("By"),
	)
	fileCacheTableCount, _ := meter.Int64Gauge(
		"pebble_file_cache_table_count",
		metric.WithDescription("Tables in file cache"),
		metric.WithUnit("{count}"),
	)
	fileCacheBlobFileCount, _ := meter.Int64Gauge(
		"pebble_file_cache_blob_file_count",
		metric.WithDescription("Blob files in file cache"),
		metric.WithUnit("{count}"),
	)
	fileCacheHits, _ := meter.Int64Counter(
		"pebble_file_cache_hits",
		metric.WithDescription("File cache hits"),
		metric.WithUnit("{count}"),
	)
	fileCacheMisses, _ := meter.Int64Counter(
		"pebble_file_cache_misses",
		metric.WithDescription("File cache misses"),
		metric.WithUnit("{count}"),
	)
	walFailoverDirSwitchCount, _ := meter.Int64Counter(
		"pebble_wal_failover_dir_switch_count",
		metric.WithDescription("WAL directory switches (failover/failback)"),
		metric.WithUnit("{count}"),
	)
	walFailoverPrimaryDuration, _ := meter.Float64Gauge(
		"pebble_wal_failover_primary_duration",
		metric.WithDescription("Cumulative WAL write duration on primary"),
		metric.WithUnit("s"),
	)
	walFailoverSecondaryDuration, _ := meter.Float64Gauge(
		"pebble_wal_failover_secondary_duration",
		metric.WithDescription("Cumulative WAL write duration on secondary"),
		metric.WithUnit("s"),
	)
	numVirtual, _ := meter.Int64Gauge(
		"pebble_num_virtual",
		metric.WithDescription("Total virtual sstable count"),
		metric.WithUnit("{count}"),
	)
	virtualSize, _ := meter.Int64Gauge(
		"pebble_virtual_size",
		metric.WithDescription("Total virtual sstable size"),
		metric.WithUnit("By"),
	)
	remoteTablesCount, _ := meter.Int64Gauge(
		"pebble_remote_tables_count",
		metric.WithDescription("Remote tables count"),
		metric.WithUnit("{count}"),
	)
	remoteTablesSize, _ := meter.Int64Gauge(
		"pebble_remote_tables_size",
		metric.WithDescription("Remote tables size"),
		metric.WithUnit("By"),
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
	snapshotEarliestSeqNum, _ := meter.Int64Gauge(
		"pebble_snapshot_earliest_seq_num",
		metric.WithDescription("Sequence number of earliest open snapshot"),
		metric.WithUnit("{count}"),
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

		compactionCount:                 compactionCount,
		compactionDuration:              compactionDuration,
		compactionBytesRead:             compactionBytesRead,
		compactionBytesWritten:          compactionBytesWritten,
		compactionEstimatedDebt:         compactionEstimatedDebt,
		compactionInProgressBytes:       compactionInProgressBytes,
		compactionNumInProgress:         compactionNumInProgress,
		compactionCancelledCount:        compactionCancelledCount,
		compactionCancelledBytes:        compactionCancelledBytes,
		compactionFailedCount:           compactionFailedCount,
		compactionDefaultCount:          compactionDefaultCount,
		compactionDeleteOnlyCount:       compactionDeleteOnlyCount,
		compactionElisionOnlyCount:      compactionElisionOnlyCount,
		compactionCopyCount:             compactionCopyCount,
		compactionMoveCount:             compactionMoveCount,
		compactionReadCount:             compactionReadCount,
		compactionTombstoneDensityCount: compactionTombstoneDensityCount,
		compactionRewriteCount:          compactionRewriteCount,
		compactionMultiLevelCount:       compactionMultiLevelCount,
		compactionBlobFileRewriteCount:  compactionBlobFileRewriteCount,
		compactionCounterLevelCount:     compactionCounterLevelCount,
		compactionNumProblemSpans:       compactionNumProblemSpans,
		compactionMarkedFiles:           compactionMarkedFiles,

		ingestCount: ingestCount,

		flushCount:              flushCount,
		flushDuration:           flushDuration,
		flushBytesWritten:       flushBytesWritten,
		flushNumInProgress:      flushNumInProgress,
		flushAsIngestCount:      flushAsIngestCount,
		flushAsIngestTableCount: flushAsIngestTableCount,
		flushAsIngestBytes:      flushAsIngestBytes,
		flushIdleDuration:       flushIdleDuration,

		filterHits:   filterHits,
		filterMisses: filterMisses,

		sstableCount:                   sstableCount,
		sstableTotalSize:               sstableTotalSize,
		sstableSublevels:               sstableSublevels,
		sstableScore:                   sstableScore,
		sstableFillFactor:              sstableFillFactor,
		sstableVirtualCount:            sstableVirtualCount,
		sstableVirtualSize:             sstableVirtualSize,
		sstableBytesIngested:           sstableBytesIngested,
		sstableBytesMoved:              sstableBytesMoved,
		sstableBytesRead:               sstableBytesRead,
		sstableBytesFlushed:            sstableBytesFlushed,
		sstableTablesCompacted:         sstableTablesCompacted,
		sstableTablesFlushed:           sstableTablesFlushed,
		sstableTablesIngested:          sstableTablesIngested,
		sstableTablesMoved:             sstableTablesMoved,
		sstableCompensatedFillFactor:   sstableCompensatedFillFactor,
		sstableEstimatedReferencesSize: sstableEstimatedReferencesSize,
		sstableTablesDeleted:           sstableTablesDeleted,
		sstableTablesExcised:           sstableTablesExcised,
		sstableBlobBytesReadEstimate:   sstableBlobBytesReadEstimate,
		sstableBlobBytesCompacted:      sstableBlobBytesCompacted,
		sstableBlobBytesFlushed:        sstableBlobBytesFlushed,
		sstableMultiLevelBytesInTop:    sstableMultiLevelBytesInTop,
		sstableMultiLevelBytesIn:       sstableMultiLevelBytesIn,
		sstableMultiLevelBytesRead:     sstableMultiLevelBytesRead,
		sstableValueBlocksSize:         sstableValueBlocksSize,
		sstableBytesWrittenDataBlocks:  sstableBytesWrittenDataBlocks,
		sstableBytesWrittenValueBlocks: sstableBytesWrittenValueBlocks,

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

		tableObsoleteSize:                  tableObsoleteSize,
		tableObsoleteCount:                 tableObsoleteCount,
		tableZombieSize:                    tableZombieSize,
		tableZombieCount:                   tableZombieCount,
		tableLiveSize:                      tableLiveSize,
		tableLiveCount:                     tableLiveCount,
		tableBackingCount:                  tableBackingCount,
		tableBackingSize:                   tableBackingSize,
		tableCompressedUnknown:             tableCompressedUnknown,
		tableCompressedSnappy:              tableCompressedSnappy,
		tableCompressedZstd:                tableCompressedZstd,
		tableCompressedMinLZ:               tableCompressedMinLZ,
		tableCompressedNone:                tableCompressedNone,
		tableLocalObsoleteSize:             tableLocalObsoleteSize,
		tableLocalObsoleteCount:            tableLocalObsoleteCount,
		tableLocalZombieSize:               tableLocalZombieSize,
		tableLocalZombieCount:              tableLocalZombieCount,
		tableGarbagePointDeletionsEstimate: tableGarbagePointDeletionsEstimate,
		tableGarbageRangeDeletionsEstimate: tableGarbageRangeDeletionsEstimate,
		tableInitialStatsComplete:          tableInitialStatsComplete,
		tablePendingStatsCount:             tablePendingStatsCount,
		blobFilesLiveCount:                 blobFilesLiveCount,
		blobFilesLiveSize:                  blobFilesLiveSize,
		blobFilesValueSize:                 blobFilesValueSize,
		blobFilesReferencedValueSize:       blobFilesReferencedValueSize,
		blobFilesObsoleteCount:             blobFilesObsoleteCount,
		blobFilesObsoleteSize:              blobFilesObsoleteSize,
		blobFilesZombieCount:               blobFilesZombieCount,
		blobFilesZombieSize:                blobFilesZombieSize,
		blobFilesLocalLiveSize:             blobFilesLocalLiveSize,
		blobFilesLocalLiveCount:            blobFilesLocalLiveCount,
		blobFilesLocalObsoleteSize:         blobFilesLocalObsoleteSize,
		blobFilesLocalObsoleteCount:        blobFilesLocalObsoleteCount,
		blobFilesLocalZombieSize:           blobFilesLocalZombieSize,
		blobFilesLocalZombieCount:          blobFilesLocalZombieCount,
		fileCacheSize:                      fileCacheSize,
		fileCacheTableCount:                fileCacheTableCount,
		fileCacheBlobFileCount:             fileCacheBlobFileCount,
		fileCacheHits:                      fileCacheHits,
		fileCacheMisses:                    fileCacheMisses,
		walFailoverDirSwitchCount:          walFailoverDirSwitchCount,
		walFailoverPrimaryDuration:         walFailoverPrimaryDuration,
		walFailoverSecondaryDuration:       walFailoverSecondaryDuration,
		numVirtual:                         numVirtual,
		virtualSize:                        virtualSize,
		remoteTablesCount:                  remoteTablesCount,
		remoteTablesSize:                   remoteTablesSize,

		keysRangeKeySetsCount:       keysRangeKeySetsCount,
		keysTombstoneCount:          keysTombstoneCount,
		keysMissizedTombstonesCount: keysMissizedTombstonesCount,

		snapshotCount:          snapshotCount,
		snapshotPinnedKeys:     snapshotPinnedKeys,
		snapshotPinnedSize:     snapshotPinnedSize,
		snapshotEarliestSeqNum: snapshotEarliestSeqNum,

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

// addDelta computes the difference between current and prev, updates prev to current,
// and adds the positive delta to the counter. Used to convert cumulative scraped
// values into rate/counter increments.
func addDelta(ctx context.Context, counter metric.Int64Counter, current int64, prev *int64, opts ...metric.AddOption) {
	delta := current - *prev
	*prev = current
	if delta > 0 {
		counter.Add(ctx, delta, opts...)
	}
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
		addDelta(ctx, pm.compactionCount, m.Compact.Count,
			&pm.prevCompactionCount, metric.WithAttributes(dbAttr))
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
		addDelta(ctx, pm.compactionCancelledCount, m.Compact.CancelledCount,
			&pm.prevCompactionCancelledCount, metric.WithAttributes(dbAttr))
	}
	if pm.compactionCancelledBytes != nil {
		addDelta(ctx, pm.compactionCancelledBytes, m.Compact.CancelledBytes,
			&pm.prevCompactionCancelledBytes, metric.WithAttributes(dbAttr))
	}
	if pm.compactionFailedCount != nil {
		addDelta(ctx, pm.compactionFailedCount, m.Compact.FailedCount,
			&pm.prevCompactionFailedCount, metric.WithAttributes(dbAttr))
	}
	if pm.compactionDefaultCount != nil {
		addDelta(ctx, pm.compactionDefaultCount, m.Compact.DefaultCount,
			&pm.prevCompactionDefaultCount, metric.WithAttributes(dbAttr))
	}
	if pm.compactionDeleteOnlyCount != nil {
		addDelta(ctx, pm.compactionDeleteOnlyCount, m.Compact.DeleteOnlyCount,
			&pm.prevCompactionDeleteOnlyCount, metric.WithAttributes(dbAttr))
	}
	if pm.compactionElisionOnlyCount != nil {
		addDelta(ctx, pm.compactionElisionOnlyCount, m.Compact.ElisionOnlyCount,
			&pm.prevCompactionElisionOnlyCount, metric.WithAttributes(dbAttr))
	}
	if pm.compactionCopyCount != nil {
		addDelta(ctx, pm.compactionCopyCount, m.Compact.CopyCount,
			&pm.prevCompactionCopyCount, metric.WithAttributes(dbAttr))
	}
	if pm.compactionMoveCount != nil {
		addDelta(ctx, pm.compactionMoveCount, m.Compact.MoveCount,
			&pm.prevCompactionMoveCount, metric.WithAttributes(dbAttr))
	}
	if pm.compactionReadCount != nil {
		addDelta(ctx, pm.compactionReadCount, m.Compact.ReadCount,
			&pm.prevCompactionReadCount, metric.WithAttributes(dbAttr))
	}
	if pm.compactionTombstoneDensityCount != nil {
		addDelta(ctx, pm.compactionTombstoneDensityCount, m.Compact.TombstoneDensityCount,
			&pm.prevCompactionTombstoneDensityCount, metric.WithAttributes(dbAttr))
	}
	if pm.compactionRewriteCount != nil {
		addDelta(ctx, pm.compactionRewriteCount, m.Compact.RewriteCount,
			&pm.prevCompactionRewriteCount, metric.WithAttributes(dbAttr))
	}
	if pm.compactionMultiLevelCount != nil {
		addDelta(ctx, pm.compactionMultiLevelCount, m.Compact.MultiLevelCount,
			&pm.prevCompactionMultiLevelCount, metric.WithAttributes(dbAttr))
	}
	if pm.compactionBlobFileRewriteCount != nil {
		addDelta(ctx, pm.compactionBlobFileRewriteCount, m.Compact.BlobFileRewriteCount,
			&pm.prevCompactionBlobFileRewriteCount, metric.WithAttributes(dbAttr))
	}
	if pm.compactionCounterLevelCount != nil {
		addDelta(ctx, pm.compactionCounterLevelCount, m.Compact.CounterLevelCount,
			&pm.prevCompactionCounterLevelCount, metric.WithAttributes(dbAttr))
	}
	if pm.compactionNumProblemSpans != nil {
		pm.compactionNumProblemSpans.Record(ctx, int64(m.Compact.NumProblemSpans), metric.WithAttributes(dbAttr))
	}
	if pm.compactionMarkedFiles != nil {
		pm.compactionMarkedFiles.Record(ctx, int64(m.Compact.MarkedFiles), metric.WithAttributes(dbAttr))
	}

	if pm.ingestCount != nil {
		addDelta(ctx, pm.ingestCount, uint64ToInt64Clamped(m.Ingest.Count),
			&pm.prevIngestCount, metric.WithAttributes(dbAttr))
	}

	if pm.flushCount != nil {
		addDelta(ctx, pm.flushCount, m.Flush.Count, &pm.prevFlushCount, metric.WithAttributes(dbAttr))
	}
	if pm.flushDuration != nil {
		pm.flushDuration.Record(ctx,
			m.Flush.WriteThroughput.WorkDuration.Seconds(), metric.WithAttributes(dbAttr))
	}
	if pm.flushBytesWritten != nil {
		addDelta(ctx, pm.flushBytesWritten, m.Flush.WriteThroughput.Bytes,
			&pm.prevFlushBytesWritten, metric.WithAttributes(dbAttr))
	}
	if pm.flushNumInProgress != nil {
		pm.flushNumInProgress.Record(ctx, m.Flush.NumInProgress, metric.WithAttributes(dbAttr))
	}
	if pm.flushAsIngestCount != nil {
		addDelta(ctx, pm.flushAsIngestCount, uint64ToInt64Clamped(m.Flush.AsIngestCount),
			&pm.prevFlushAsIngestCount, metric.WithAttributes(dbAttr))
	}
	if pm.flushAsIngestTableCount != nil {
		addDelta(ctx, pm.flushAsIngestTableCount, uint64ToInt64Clamped(m.Flush.AsIngestTableCount),
			&pm.prevFlushAsIngestTableCount, metric.WithAttributes(dbAttr))
	}
	if pm.flushAsIngestBytes != nil {
		addDelta(ctx, pm.flushAsIngestBytes, uint64ToInt64Clamped(m.Flush.AsIngestBytes),
			&pm.prevFlushAsIngestBytes, metric.WithAttributes(dbAttr))
	}
	if pm.flushIdleDuration != nil {
		pm.flushIdleDuration.Record(ctx,
			m.Flush.WriteThroughput.IdleDuration.Seconds(), metric.WithAttributes(dbAttr))
	}

	if pm.filterHits != nil {
		addDelta(ctx, pm.filterHits, m.Filter.Hits, &pm.prevFilterHits, metric.WithAttributes(dbAttr))
	}
	if pm.filterMisses != nil {
		addDelta(ctx, pm.filterMisses, m.Filter.Misses, &pm.prevFilterMisses, metric.WithAttributes(dbAttr))
	}

	for level := 0; level < len(m.Levels); level++ {
		lm := m.Levels[level]
		levelAttr := attribute.Int("level", level)
		attrs := metric.WithAttributes(dbAttr, levelAttr)

		// Grow prev slices if needed.
		for level >= len(pm.prevCompactionBytesReadByLevel) {
			pm.prevCompactionBytesReadByLevel = append(pm.prevCompactionBytesReadByLevel, 0)
			pm.prevCompactionBytesWrittenByLevel = append(pm.prevCompactionBytesWrittenByLevel, 0)
			pm.prevSstableBytesIngestedByLevel = append(pm.prevSstableBytesIngestedByLevel, 0)
			pm.prevSstableBytesMovedByLevel = append(pm.prevSstableBytesMovedByLevel, 0)
			pm.prevSstableBytesReadByLevel = append(pm.prevSstableBytesReadByLevel, 0)
			pm.prevSstableBytesFlushedByLevel = append(pm.prevSstableBytesFlushedByLevel, 0)
			pm.prevSstableTablesCompactedByLevel = append(pm.prevSstableTablesCompactedByLevel, 0)
			pm.prevSstableTablesFlushedByLevel = append(pm.prevSstableTablesFlushedByLevel, 0)
			pm.prevSstableTablesIngestedByLevel = append(pm.prevSstableTablesIngestedByLevel, 0)
			pm.prevSstableTablesMovedByLevel = append(pm.prevSstableTablesMovedByLevel, 0)
			pm.prevSstableTablesDeletedByLevel = append(pm.prevSstableTablesDeletedByLevel, 0)
			pm.prevSstableTablesExcisedByLevel = append(pm.prevSstableTablesExcisedByLevel, 0)
			pm.prevSstableBlobBytesReadEstimateByLevel = append(pm.prevSstableBlobBytesReadEstimateByLevel, 0)
			pm.prevSstableBlobBytesCompactedByLevel = append(pm.prevSstableBlobBytesCompactedByLevel, 0)
			pm.prevSstableBlobBytesFlushedByLevel = append(pm.prevSstableBlobBytesFlushedByLevel, 0)
			pm.prevSstableMultiLevelBytesInTopByLevel = append(pm.prevSstableMultiLevelBytesInTopByLevel, 0)
			pm.prevSstableMultiLevelBytesInByLevel = append(pm.prevSstableMultiLevelBytesInByLevel, 0)
			pm.prevSstableMultiLevelBytesReadByLevel = append(pm.prevSstableMultiLevelBytesReadByLevel, 0)
			pm.prevSstableBytesWrittenDataBlocksByLevel = append(pm.prevSstableBytesWrittenDataBlocksByLevel, 0)
			pm.prevSstableBytesWrittenValueBlocksByLevel = append(pm.prevSstableBytesWrittenValueBlocksByLevel, 0)
		}

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
			addDelta(ctx, pm.compactionBytesRead, uint64ToInt64Clamped(lm.TableBytesIn),
				&pm.prevCompactionBytesReadByLevel[level], attrs)
		}
		if pm.compactionBytesWritten != nil {
			addDelta(ctx, pm.compactionBytesWritten, uint64ToInt64Clamped(lm.TableBytesCompacted),
				&pm.prevCompactionBytesWrittenByLevel[level], attrs)
		}
		if pm.sstableBytesIngested != nil {
			addDelta(ctx, pm.sstableBytesIngested, uint64ToInt64Clamped(lm.TableBytesIngested),
				&pm.prevSstableBytesIngestedByLevel[level], attrs)
		}
		if pm.sstableBytesMoved != nil {
			addDelta(ctx, pm.sstableBytesMoved, uint64ToInt64Clamped(lm.TableBytesMoved),
				&pm.prevSstableBytesMovedByLevel[level], attrs)
		}
		if pm.sstableBytesRead != nil {
			addDelta(ctx, pm.sstableBytesRead, uint64ToInt64Clamped(lm.TableBytesRead),
				&pm.prevSstableBytesReadByLevel[level], attrs)
		}
		if pm.sstableBytesFlushed != nil {
			addDelta(ctx, pm.sstableBytesFlushed, uint64ToInt64Clamped(lm.TableBytesFlushed),
				&pm.prevSstableBytesFlushedByLevel[level], attrs)
		}
		if pm.sstableTablesCompacted != nil {
			addDelta(ctx, pm.sstableTablesCompacted, uint64ToInt64Clamped(lm.TablesCompacted),
				&pm.prevSstableTablesCompactedByLevel[level], attrs)
		}
		if pm.sstableTablesFlushed != nil {
			addDelta(ctx, pm.sstableTablesFlushed, uint64ToInt64Clamped(lm.TablesFlushed),
				&pm.prevSstableTablesFlushedByLevel[level], attrs)
		}
		if pm.sstableTablesIngested != nil {
			addDelta(ctx, pm.sstableTablesIngested, uint64ToInt64Clamped(lm.TablesIngested),
				&pm.prevSstableTablesIngestedByLevel[level], attrs)
		}
		if pm.sstableTablesMoved != nil {
			addDelta(ctx, pm.sstableTablesMoved, uint64ToInt64Clamped(lm.TablesMoved),
				&pm.prevSstableTablesMovedByLevel[level], attrs)
		}
		if pm.sstableCompensatedFillFactor != nil {
			pm.sstableCompensatedFillFactor.Record(ctx, lm.CompensatedFillFactor, attrs)
		}
		if pm.sstableEstimatedReferencesSize != nil {
			pm.sstableEstimatedReferencesSize.Record(ctx, uint64ToInt64Clamped(lm.EstimatedReferencesSize), attrs)
		}
		if pm.sstableTablesDeleted != nil {
			addDelta(ctx, pm.sstableTablesDeleted, uint64ToInt64Clamped(lm.TablesDeleted),
				&pm.prevSstableTablesDeletedByLevel[level], attrs)
		}
		if pm.sstableTablesExcised != nil {
			addDelta(ctx, pm.sstableTablesExcised, uint64ToInt64Clamped(lm.TablesExcised),
				&pm.prevSstableTablesExcisedByLevel[level], attrs)
		}
		if pm.sstableBlobBytesReadEstimate != nil {
			addDelta(ctx, pm.sstableBlobBytesReadEstimate, uint64ToInt64Clamped(lm.BlobBytesReadEstimate),
				&pm.prevSstableBlobBytesReadEstimateByLevel[level], attrs)
		}
		if pm.sstableBlobBytesCompacted != nil {
			addDelta(ctx, pm.sstableBlobBytesCompacted, uint64ToInt64Clamped(lm.BlobBytesCompacted),
				&pm.prevSstableBlobBytesCompactedByLevel[level], attrs)
		}
		if pm.sstableBlobBytesFlushed != nil {
			addDelta(ctx, pm.sstableBlobBytesFlushed, uint64ToInt64Clamped(lm.BlobBytesFlushed),
				&pm.prevSstableBlobBytesFlushedByLevel[level], attrs)
		}
		if pm.sstableMultiLevelBytesInTop != nil {
			addDelta(ctx, pm.sstableMultiLevelBytesInTop, uint64ToInt64Clamped(lm.MultiLevel.TableBytesInTop),
				&pm.prevSstableMultiLevelBytesInTopByLevel[level], attrs)
		}
		if pm.sstableMultiLevelBytesIn != nil {
			addDelta(ctx, pm.sstableMultiLevelBytesIn, uint64ToInt64Clamped(lm.MultiLevel.TableBytesIn),
				&pm.prevSstableMultiLevelBytesInByLevel[level], attrs)
		}
		if pm.sstableMultiLevelBytesRead != nil {
			addDelta(ctx, pm.sstableMultiLevelBytesRead, uint64ToInt64Clamped(lm.MultiLevel.TableBytesRead),
				&pm.prevSstableMultiLevelBytesReadByLevel[level], attrs)
		}
		if pm.sstableValueBlocksSize != nil {
			pm.sstableValueBlocksSize.Record(ctx, uint64ToInt64Clamped(lm.Additional.ValueBlocksSize), attrs)
		}
		if pm.sstableBytesWrittenDataBlocks != nil {
			addDelta(ctx, pm.sstableBytesWrittenDataBlocks,
				uint64ToInt64Clamped(lm.Additional.BytesWrittenDataBlocks),
				&pm.prevSstableBytesWrittenDataBlocksByLevel[level], attrs)
		}
		if pm.sstableBytesWrittenValueBlocks != nil {
			addDelta(ctx, pm.sstableBytesWrittenValueBlocks,
				uint64ToInt64Clamped(lm.Additional.BytesWrittenValueBlocks),
				&pm.prevSstableBytesWrittenValueBlocksByLevel[level], attrs)
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
		addDelta(ctx, pm.walBytesIn,
			uint64ToInt64Clamped(m.WAL.BytesIn), &pm.prevWalBytesIn, metric.WithAttributes(dbAttr))
	}
	if pm.walBytesWritten != nil {
		addDelta(ctx, pm.walBytesWritten, uint64ToInt64Clamped(m.WAL.BytesWritten),
			&pm.prevWalBytesWritten, metric.WithAttributes(dbAttr))
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
	if pm.tableBackingCount != nil {
		pm.tableBackingCount.Record(ctx,
			uint64ToInt64Clamped(m.Table.BackingTableCount), metric.WithAttributes(dbAttr))
	}
	if pm.tableBackingSize != nil {
		pm.tableBackingSize.Record(ctx, uint64ToInt64Clamped(m.Table.BackingTableSize), metric.WithAttributes(dbAttr))
	}
	if pm.tableCompressedUnknown != nil {
		pm.tableCompressedUnknown.Record(ctx, m.Table.CompressedCountUnknown, metric.WithAttributes(dbAttr))
	}
	if pm.tableCompressedSnappy != nil {
		pm.tableCompressedSnappy.Record(ctx, m.Table.CompressedCountSnappy, metric.WithAttributes(dbAttr))
	}
	if pm.tableCompressedZstd != nil {
		pm.tableCompressedZstd.Record(ctx, m.Table.CompressedCountZstd, metric.WithAttributes(dbAttr))
	}
	if pm.tableCompressedMinLZ != nil {
		pm.tableCompressedMinLZ.Record(ctx, m.Table.CompressedCountMinLZ, metric.WithAttributes(dbAttr))
	}
	if pm.tableCompressedNone != nil {
		pm.tableCompressedNone.Record(ctx, m.Table.CompressedCountNone, metric.WithAttributes(dbAttr))
	}
	if pm.tableLocalObsoleteSize != nil {
		pm.tableLocalObsoleteSize.Record(ctx,
			uint64ToInt64Clamped(m.Table.Local.ObsoleteSize), metric.WithAttributes(dbAttr))
	}
	if pm.tableLocalObsoleteCount != nil {
		pm.tableLocalObsoleteCount.Record(ctx,
			uint64ToInt64Clamped(m.Table.Local.ObsoleteCount), metric.WithAttributes(dbAttr))
	}
	if pm.tableLocalZombieSize != nil {
		pm.tableLocalZombieSize.Record(ctx,
			uint64ToInt64Clamped(m.Table.Local.ZombieSize), metric.WithAttributes(dbAttr))
	}
	if pm.tableLocalZombieCount != nil {
		pm.tableLocalZombieCount.Record(ctx,
			uint64ToInt64Clamped(m.Table.Local.ZombieCount), metric.WithAttributes(dbAttr))
	}
	if pm.tableGarbagePointDeletionsEstimate != nil {
		pm.tableGarbagePointDeletionsEstimate.Record(ctx,
			uint64ToInt64Clamped(m.Table.Garbage.PointDeletionsBytesEstimate), metric.WithAttributes(dbAttr))
	}
	if pm.tableGarbageRangeDeletionsEstimate != nil {
		pm.tableGarbageRangeDeletionsEstimate.Record(ctx,
			uint64ToInt64Clamped(m.Table.Garbage.RangeDeletionsBytesEstimate), metric.WithAttributes(dbAttr))
	}
	if pm.tableInitialStatsComplete != nil {
		v := int64(0)
		if m.Table.InitialStatsCollectionComplete {
			v = 1
		}
		pm.tableInitialStatsComplete.Record(ctx, v, metric.WithAttributes(dbAttr))
	}
	if pm.tablePendingStatsCount != nil {
		pm.tablePendingStatsCount.Record(ctx, m.Table.PendingStatsCollectionCount, metric.WithAttributes(dbAttr))
	}
	if pm.blobFilesLiveCount != nil {
		pm.blobFilesLiveCount.Record(ctx, uint64ToInt64Clamped(m.BlobFiles.LiveCount), metric.WithAttributes(dbAttr))
	}
	if pm.blobFilesLiveSize != nil {
		pm.blobFilesLiveSize.Record(ctx, uint64ToInt64Clamped(m.BlobFiles.LiveSize), metric.WithAttributes(dbAttr))
	}
	if pm.blobFilesValueSize != nil {
		pm.blobFilesValueSize.Record(ctx, uint64ToInt64Clamped(m.BlobFiles.ValueSize), metric.WithAttributes(dbAttr))
	}
	if pm.blobFilesReferencedValueSize != nil {
		pm.blobFilesReferencedValueSize.Record(ctx,
			uint64ToInt64Clamped(m.BlobFiles.ReferencedValueSize), metric.WithAttributes(dbAttr))
	}
	if pm.blobFilesObsoleteCount != nil {
		pm.blobFilesObsoleteCount.Record(ctx,
			uint64ToInt64Clamped(m.BlobFiles.ObsoleteCount), metric.WithAttributes(dbAttr))
	}
	if pm.blobFilesObsoleteSize != nil {
		pm.blobFilesObsoleteSize.Record(ctx,
			uint64ToInt64Clamped(m.BlobFiles.ObsoleteSize), metric.WithAttributes(dbAttr))
	}
	if pm.blobFilesZombieCount != nil {
		pm.blobFilesZombieCount.Record(ctx,
			uint64ToInt64Clamped(m.BlobFiles.ZombieCount), metric.WithAttributes(dbAttr))
	}
	if pm.blobFilesZombieSize != nil {
		pm.blobFilesZombieSize.Record(ctx,
			uint64ToInt64Clamped(m.BlobFiles.ZombieSize), metric.WithAttributes(dbAttr))
	}
	if pm.blobFilesLocalLiveSize != nil {
		pm.blobFilesLocalLiveSize.Record(ctx,
			uint64ToInt64Clamped(m.BlobFiles.Local.LiveSize), metric.WithAttributes(dbAttr))
	}
	if pm.blobFilesLocalLiveCount != nil {
		pm.blobFilesLocalLiveCount.Record(ctx,
			uint64ToInt64Clamped(m.BlobFiles.Local.LiveCount), metric.WithAttributes(dbAttr))
	}
	if pm.blobFilesLocalObsoleteSize != nil {
		pm.blobFilesLocalObsoleteSize.Record(ctx,
			uint64ToInt64Clamped(m.BlobFiles.Local.ObsoleteSize), metric.WithAttributes(dbAttr))
	}
	if pm.blobFilesLocalObsoleteCount != nil {
		pm.blobFilesLocalObsoleteCount.Record(ctx,
			uint64ToInt64Clamped(m.BlobFiles.Local.ObsoleteCount), metric.WithAttributes(dbAttr))
	}
	if pm.blobFilesLocalZombieSize != nil {
		pm.blobFilesLocalZombieSize.Record(ctx,
			uint64ToInt64Clamped(m.BlobFiles.Local.ZombieSize), metric.WithAttributes(dbAttr))
	}
	if pm.blobFilesLocalZombieCount != nil {
		pm.blobFilesLocalZombieCount.Record(ctx,
			uint64ToInt64Clamped(m.BlobFiles.Local.ZombieCount), metric.WithAttributes(dbAttr))
	}
	if pm.fileCacheSize != nil {
		pm.fileCacheSize.Record(ctx, m.FileCache.Size, metric.WithAttributes(dbAttr))
	}
	if pm.fileCacheTableCount != nil {
		pm.fileCacheTableCount.Record(ctx, m.FileCache.TableCount, metric.WithAttributes(dbAttr))
	}
	if pm.fileCacheBlobFileCount != nil {
		pm.fileCacheBlobFileCount.Record(ctx, m.FileCache.BlobFileCount, metric.WithAttributes(dbAttr))
	}
	if pm.fileCacheHits != nil {
		addDelta(ctx, pm.fileCacheHits, m.FileCache.Hits, &pm.prevFileCacheHits, metric.WithAttributes(dbAttr))
	}
	if pm.fileCacheMisses != nil {
		addDelta(ctx, pm.fileCacheMisses,
			m.FileCache.Misses, &pm.prevFileCacheMisses, metric.WithAttributes(dbAttr))
	}
	if pm.walFailoverDirSwitchCount != nil {
		addDelta(ctx, pm.walFailoverDirSwitchCount, m.WAL.Failover.DirSwitchCount,
			&pm.prevWalFailoverDirSwitchCount, metric.WithAttributes(dbAttr))
	}
	if pm.walFailoverPrimaryDuration != nil {
		pm.walFailoverPrimaryDuration.Record(ctx,
			m.WAL.Failover.PrimaryWriteDuration.Seconds(), metric.WithAttributes(dbAttr))
	}
	if pm.walFailoverSecondaryDuration != nil {
		pm.walFailoverSecondaryDuration.Record(ctx,
			m.WAL.Failover.SecondaryWriteDuration.Seconds(), metric.WithAttributes(dbAttr))
	}
	if pm.numVirtual != nil {
		pm.numVirtual.Record(ctx, uint64ToInt64Clamped(m.NumVirtual()), metric.WithAttributes(dbAttr))
	}
	if pm.virtualSize != nil {
		pm.virtualSize.Record(ctx, uint64ToInt64Clamped(m.VirtualSize()), metric.WithAttributes(dbAttr))
	}
	rtCount, rtSize := m.RemoteTablesTotal()
	if pm.remoteTablesCount != nil {
		pm.remoteTablesCount.Record(ctx, uint64ToInt64Clamped(rtCount), metric.WithAttributes(dbAttr))
	}
	if pm.remoteTablesSize != nil {
		pm.remoteTablesSize.Record(ctx, uint64ToInt64Clamped(rtSize), metric.WithAttributes(dbAttr))
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
		addDelta(ctx, pm.keysMissizedTombstonesCount, uint64ToInt64Clamped(m.Keys.MissizedTombstonesCount),
			&pm.prevKeysMissizedTombstonesCount, metric.WithAttributes(dbAttr))
	}

	if pm.snapshotCount != nil {
		pm.snapshotCount.Record(ctx, int64(m.Snapshots.Count), metric.WithAttributes(dbAttr))
	}
	if pm.snapshotPinnedKeys != nil {
		addDelta(ctx, pm.snapshotPinnedKeys, uint64ToInt64Clamped(m.Snapshots.PinnedKeys),
			&pm.prevSnapshotPinnedKeys, metric.WithAttributes(dbAttr))
	}
	if pm.snapshotPinnedSize != nil {
		addDelta(ctx, pm.snapshotPinnedSize, uint64ToInt64Clamped(m.Snapshots.PinnedSize),
			&pm.prevSnapshotPinnedSize, metric.WithAttributes(dbAttr))
	}
	if pm.snapshotEarliestSeqNum != nil {
		pm.snapshotEarliestSeqNum.Record(ctx,
			uint64ToInt64Clamped(uint64(m.Snapshots.EarliestSeqNum)), metric.WithAttributes(dbAttr))
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
		addDelta(ctx, pm.cacheHits, m.BlockCache.Hits, &pm.prevCacheHits, metric.WithAttributes(dbAttr))
	}
	if pm.cacheMisses != nil {
		addDelta(ctx, pm.cacheMisses, m.BlockCache.Misses, &pm.prevCacheMisses, metric.WithAttributes(dbAttr))
	}
	if pm.cacheSize != nil {
		pm.cacheSize.Record(ctx, m.BlockCache.Size, metric.WithAttributes(dbAttr))
	}
}
