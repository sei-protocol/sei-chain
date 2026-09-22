package pebbledb

import (
	"context"
	"sync"
	"sync/atomic"
	"time"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/metric"

	"github.com/cockroachdb/pebble/v2"
)

const pebbleMeterName = "seidb_pebble"

// numLevels is how many LSM levels a Pebble snapshot reports.
const numLevels = len(pebble.Metrics{}.Levels)

type pebbleMetrics struct {
	meter metric.Meter
	insts []metric.Observable

	dbAttrs    metric.ObserveOption
	levelAttrs [numLevels]metric.ObserveOption

	snapshot atomic.Pointer[pebble.Metrics]
	report   []func(metric.Observer, *pebble.Metrics)
}

// NewPebbleMetrics registers OTel observables over a Pebble metrics snapshot
func NewPebbleMetrics(db *pebble.DB, databaseName string, refreshInterval time.Duration) func() {
	meter := otel.Meter(pebbleMeterName)
	dbAttr := attribute.String("db", databaseName)
	p := &pebbleMetrics{meter: meter, dbAttrs: metric.WithAttributes(dbAttr)}
	for level := range p.levelAttrs {
		p.levelAttrs[level] = metric.WithAttributes(dbAttr, attribute.Int("level", level))
	}
	p.declareDB()
	p.declareLevels()

	p.snapshot.Store(db.Metrics())
	observe := func(_ context.Context, o metric.Observer) error {
		m := p.snapshot.Load()
		for _, report := range p.report {
			report(o, m)
		}
		return nil
	}

	reg, err := meter.RegisterCallback(observe, p.insts...)
	if err != nil {
		otel.Handle(err)
		return func() {}
	}

	ticker := time.NewTicker(refreshInterval)
	stop, stopped := make(chan struct{}), make(chan struct{})
	go func() {
		defer close(stopped)
		defer ticker.Stop()
		for {
			select {
			case <-stop:
				return
			case <-ticker.C:
				p.snapshot.Store(db.Metrics())
			}
		}
	}()
	// Waiting for the refresher to exit before unregistering is what lets the
	// caller close db as soon as this returns: no observation can be in flight.
	return sync.OnceFunc(func() {
		close(stop)
		<-stopped
		_ = reg.Unregister()
	})
}

// counter declares a whole-DB series whose value Pebble only ever raises.
func (p *pebbleMetrics) counter(name, unit, desc string, val func(*pebble.Metrics) float64) {
	inst, _ := p.meter.Float64ObservableCounter(name, metric.WithDescription(desc), metric.WithUnit(unit))
	p.insts = append(p.insts, inst)
	p.report = append(p.report, func(o metric.Observer, m *pebble.Metrics) {
		o.ObserveFloat64(inst, val(m), p.dbAttrs)
	})
}

// gauge declares a whole-DB series whose value can fall as well as rise.
func (p *pebbleMetrics) gauge(name, unit, desc string, val func(*pebble.Metrics) float64) {
	inst, _ := p.meter.Float64ObservableGauge(name, metric.WithDescription(desc), metric.WithUnit(unit))
	p.insts = append(p.insts, inst)
	p.report = append(p.report, func(o metric.Observer, m *pebble.Metrics) {
		o.ObserveFloat64(inst, val(m), p.dbAttrs)
	})
}

// levelCounter declares a per-level series whose value Pebble only ever raises.
func (p *pebbleMetrics) levelCounter(name, unit, desc string, val func(*pebble.LevelMetrics) float64) {
	inst, _ := p.meter.Float64ObservableCounter(name, metric.WithDescription(desc), metric.WithUnit(unit))
	p.insts = append(p.insts, inst)
	p.report = append(p.report, func(o metric.Observer, m *pebble.Metrics) {
		for level := range m.Levels {
			o.ObserveFloat64(inst, val(&m.Levels[level]), p.levelAttrs[level])
		}
	})
}

// levelGauge declares a per-level series whose value can fall as well as rise.
func (p *pebbleMetrics) levelGauge(name, unit, desc string, val func(*pebble.LevelMetrics) float64) {
	inst, _ := p.meter.Float64ObservableGauge(name, metric.WithDescription(desc), metric.WithUnit(unit))
	p.insts = append(p.insts, inst)
	p.report = append(p.report, func(o metric.Observer, m *pebble.Metrics) {
		for level := range m.Levels {
			o.ObserveFloat64(inst, val(&m.Levels[level]), p.levelAttrs[level])
		}
	})
}

// declareDB declares the series read from a whole-DB snapshot.
func (p *pebbleMetrics) declareDB() {
	p.counter("pebble_compaction_count", "{count}", "Total number of compactions",
		func(m *pebble.Metrics) float64 { return float64(m.Compact.Count) })
	p.counter("pebble_compaction_duration", "s", "Cumulative compaction duration since DB open",
		func(m *pebble.Metrics) float64 { return m.Compact.Duration.Seconds() })
	p.gauge("pebble_compaction_estimated_debt", "By", "Estimated bytes to compact for LSM to reach stable state",
		func(m *pebble.Metrics) float64 { return float64(m.Compact.EstimatedDebt) })
	p.gauge("pebble_compaction_in_progress_bytes", "By", "Bytes in sstables being written by in-progress compactions",
		func(m *pebble.Metrics) float64 { return float64(m.Compact.InProgressBytes) })
	p.gauge("pebble_compaction_num_in_progress", "{count}", "Number of compactions in progress",
		func(m *pebble.Metrics) float64 { return float64(m.Compact.NumInProgress) })
	p.counter("pebble_compaction_cancelled_count", "{count}", "Number of compactions that were cancelled",
		func(m *pebble.Metrics) float64 { return float64(m.Compact.CancelledCount) })
	p.counter("pebble_compaction_cancelled_bytes", "By", "Bytes written by cancelled compactions",
		func(m *pebble.Metrics) float64 { return float64(m.Compact.CancelledBytes) })
	p.counter("pebble_compaction_failed_count", "{count}", "Number of compactions that hit an error",
		func(m *pebble.Metrics) float64 { return float64(m.Compact.FailedCount) })
	p.counter("pebble_compaction_default_count", "{count}", "Default compactions",
		func(m *pebble.Metrics) float64 { return float64(m.Compact.DefaultCount) })
	p.counter("pebble_compaction_delete_only_count", "{count}", "Delete-only compactions",
		func(m *pebble.Metrics) float64 { return float64(m.Compact.DeleteOnlyCount) })
	p.counter("pebble_compaction_elision_only_count", "{count}", "Elision-only compactions",
		func(m *pebble.Metrics) float64 { return float64(m.Compact.ElisionOnlyCount) })
	p.counter("pebble_compaction_copy_count", "{count}", "Copy compactions",
		func(m *pebble.Metrics) float64 { return float64(m.Compact.CopyCount) })
	p.counter("pebble_compaction_move_count", "{count}", "Move compactions",
		func(m *pebble.Metrics) float64 { return float64(m.Compact.MoveCount) })
	p.counter("pebble_compaction_read_count", "{count}", "Read compactions",
		func(m *pebble.Metrics) float64 { return float64(m.Compact.ReadCount) })
	p.counter("pebble_compaction_tombstone_density_count", "{count}", "Tombstone-density compactions",
		func(m *pebble.Metrics) float64 { return float64(m.Compact.TombstoneDensityCount) })
	p.counter("pebble_compaction_rewrite_count", "{count}", "Rewrite compactions",
		func(m *pebble.Metrics) float64 { return float64(m.Compact.RewriteCount) })
	p.counter("pebble_compaction_multi_level_count", "{count}", "Multi-level compactions",
		func(m *pebble.Metrics) float64 { return float64(m.Compact.MultiLevelCount) })
	p.counter("pebble_compaction_blob_file_rewrite_count", "{count}", "Blob file rewrite compactions",
		func(m *pebble.Metrics) float64 { return float64(m.Compact.BlobFileRewriteCount) })
	p.counter("pebble_compaction_counter_level_count", "{count}", "Counter-level compactions",
		func(m *pebble.Metrics) float64 { return float64(m.Compact.CounterLevelCount) })
	p.gauge("pebble_compaction_num_problem_spans", "{count}", "Problem spans blocking compactions",
		func(m *pebble.Metrics) float64 { return float64(m.Compact.NumProblemSpans) })
	p.gauge("pebble_compaction_marked_files", "{count}", "Files marked for compaction",
		func(m *pebble.Metrics) float64 { return float64(m.Compact.MarkedFiles) })

	p.counter("pebble_ingest_count", "{count}", "Total number of ingestions",
		func(m *pebble.Metrics) float64 { return float64(m.Ingest.Count) })

	p.counter("pebble_flush_count", "{count}", "Total number of memtable flushes",
		func(m *pebble.Metrics) float64 { return float64(m.Flush.Count) })
	p.counter("pebble_flush_duration", "s", "Cumulative memtable flush work duration since DB open",
		func(m *pebble.Metrics) float64 { return m.Flush.WriteThroughput.WorkDuration.Seconds() })
	p.counter("pebble_flush_bytes_written", "By", "Total bytes written during memtable flushes",
		func(m *pebble.Metrics) float64 { return float64(m.Flush.WriteThroughput.Bytes) })
	p.gauge("pebble_flush_num_in_progress", "{count}", "Number of flushes in progress",
		func(m *pebble.Metrics) float64 { return float64(m.Flush.NumInProgress) })
	p.counter("pebble_flush_as_ingest_count", "{count}", "Flush operations handling ingested tables",
		func(m *pebble.Metrics) float64 { return float64(m.Flush.AsIngestCount) })
	p.counter("pebble_flush_as_ingest_table_count", "{count}", "Tables ingested as flushables",
		func(m *pebble.Metrics) float64 { return float64(m.Flush.AsIngestTableCount) })
	p.counter("pebble_flush_as_ingest_bytes", "By", "Bytes flushed for flushables from ingestion",
		func(m *pebble.Metrics) float64 { return float64(m.Flush.AsIngestBytes) })
	p.gauge("pebble_flush_idle_duration", "s", "Idle duration before memtable flushes",
		func(m *pebble.Metrics) float64 { return m.Flush.WriteThroughput.IdleDuration.Seconds() })

	p.counter("pebble_filter_hits", "{count}", "Bloom filter hits (block reads avoided)",
		func(m *pebble.Metrics) float64 { return float64(m.Filter.Hits) })
	p.counter("pebble_filter_misses", "{count}", "Bloom filter misses",
		func(m *pebble.Metrics) float64 { return float64(m.Filter.Misses) })

	p.gauge("pebble_memtable_count", "{count}", "Current number of memtables",
		func(m *pebble.Metrics) float64 { return float64(m.MemTable.Count) })
	p.gauge("pebble_memtable_total_size", "By", "Total size of all memtables",
		func(m *pebble.Metrics) float64 { return float64(m.MemTable.Size) })
	p.gauge("pebble_memtable_zombie_size", "By", "Bytes in zombie memtables (released but in use by iterators)",
		func(m *pebble.Metrics) float64 { return float64(m.MemTable.ZombieSize) })
	p.gauge("pebble_memtable_zombie_count", "{count}", "Count of zombie memtables",
		func(m *pebble.Metrics) float64 { return float64(m.MemTable.ZombieCount) })

	p.gauge("pebble_wal_size", "By", "Current size of Write-Ahead Log",
		func(m *pebble.Metrics) float64 { return float64(m.WAL.Size) })
	p.gauge("pebble_wal_files", "{count}", "Number of live WAL files",
		func(m *pebble.Metrics) float64 { return float64(m.WAL.Files) })
	p.gauge("pebble_wal_obsolete_files", "{count}", "Number of obsolete WAL files",
		func(m *pebble.Metrics) float64 { return float64(m.WAL.ObsoleteFiles) })
	p.gauge("pebble_wal_obsolete_physical_size", "By", "Physical size of obsolete WAL files",
		func(m *pebble.Metrics) float64 { return float64(m.WAL.ObsoletePhysicalSize) })
	p.gauge("pebble_wal_physical_size", "By", "Physical size of WAL files on disk",
		func(m *pebble.Metrics) float64 { return float64(m.WAL.PhysicalSize) })
	p.counter("pebble_wal_bytes_in", "By", "Logical bytes written to WAL",
		func(m *pebble.Metrics) float64 { return float64(m.WAL.BytesIn) })
	p.counter("pebble_wal_bytes_written", "By", "Bytes written to WAL",
		func(m *pebble.Metrics) float64 { return float64(m.WAL.BytesWritten) })
	p.counter("pebble_wal_failover_dir_switch_count", "{count}", "WAL directory switches (failover/failback)",
		func(m *pebble.Metrics) float64 { return float64(m.WAL.Failover.DirSwitchCount) })
	p.gauge("pebble_wal_failover_primary_duration", "s", "Cumulative WAL write duration on primary",
		func(m *pebble.Metrics) float64 { return m.WAL.Failover.PrimaryWriteDuration.Seconds() })
	p.gauge("pebble_wal_failover_secondary_duration", "s", "Cumulative WAL write duration on secondary",
		func(m *pebble.Metrics) float64 { return m.WAL.Failover.SecondaryWriteDuration.Seconds() })

	p.gauge("pebble_table_obsolete_size", "By", "Bytes in obsolete tables no longer referenced",
		func(m *pebble.Metrics) float64 { return float64(m.Table.ObsoleteSize) })
	p.gauge("pebble_table_obsolete_count", "{count}", "Count of obsolete tables",
		func(m *pebble.Metrics) float64 { return float64(m.Table.ObsoleteCount) })
	p.gauge("pebble_table_zombie_size", "By", "Bytes in zombie tables (released but in use by iterators)",
		func(m *pebble.Metrics) float64 { return float64(m.Table.ZombieSize) })
	p.gauge("pebble_table_zombie_count", "{count}", "Count of zombie tables",
		func(m *pebble.Metrics) float64 { return float64(m.Table.ZombieCount) })
	p.gauge("pebble_table_live_size", "By", "Bytes in live tables",
		func(m *pebble.Metrics) float64 { return float64(m.Table.Local.LiveSize) })
	p.gauge("pebble_table_live_count", "{count}", "Count of live tables",
		func(m *pebble.Metrics) float64 { return float64(m.Table.Local.LiveCount) })
	p.gauge("pebble_table_backing_count", "{count}", "Sstables backing virtual tables",
		func(m *pebble.Metrics) float64 { return float64(m.Table.BackingTableCount) })
	p.gauge("pebble_table_backing_size", "By", "Size of sstables backing virtual tables",
		func(m *pebble.Metrics) float64 { return float64(m.Table.BackingTableSize) })
	p.gauge("pebble_table_compressed_unknown", "{count}", "Sstables with unknown compression",
		func(m *pebble.Metrics) float64 { return float64(m.Table.CompressedCountUnknown) })
	p.gauge("pebble_table_compressed_snappy", "{count}", "Snappy-compressed sstables",
		func(m *pebble.Metrics) float64 { return float64(m.Table.CompressedCountSnappy) })
	p.gauge("pebble_table_compressed_zstd", "{count}", "Zstd-compressed sstables",
		func(m *pebble.Metrics) float64 { return float64(m.Table.CompressedCountZstd) })
	p.gauge("pebble_table_compressed_minlz", "{count}", "MinLZ-compressed sstables",
		func(m *pebble.Metrics) float64 { return float64(m.Table.CompressedCountMinLZ) })
	p.gauge("pebble_table_compressed_none", "{count}", "Uncompressed sstables",
		func(m *pebble.Metrics) float64 { return float64(m.Table.CompressedCountNone) })
	p.gauge("pebble_table_local_obsolete_size", "By", "Local obsolete table size",
		func(m *pebble.Metrics) float64 { return float64(m.Table.Local.ObsoleteSize) })
	p.gauge("pebble_table_local_obsolete_count", "{count}", "Local obsolete table count",
		func(m *pebble.Metrics) float64 { return float64(m.Table.Local.ObsoleteCount) })
	p.gauge("pebble_table_local_zombie_size", "By", "Local zombie table size",
		func(m *pebble.Metrics) float64 { return float64(m.Table.Local.ZombieSize) })
	p.gauge("pebble_table_local_zombie_count", "{count}", "Local zombie table count",
		func(m *pebble.Metrics) float64 { return float64(m.Table.Local.ZombieCount) })
	p.gauge("pebble_table_garbage_point_deletions_estimate", "By", "Est. bytes reclaimable from point deletes",
		func(m *pebble.Metrics) float64 { return float64(m.Table.Garbage.PointDeletionsBytesEstimate) })
	p.gauge("pebble_table_garbage_range_deletions_estimate", "By", "Est. bytes reclaimable from range deletes",
		func(m *pebble.Metrics) float64 { return float64(m.Table.Garbage.RangeDeletionsBytesEstimate) })
	p.gauge("pebble_table_initial_stats_complete", "1", "1 if initial stats collection complete",
		func(m *pebble.Metrics) float64 {
			if m.Table.InitialStatsCollectionComplete {
				return 1
			}
			return 0
		})
	p.gauge("pebble_table_pending_stats_count", "{count}", "New sstables awaiting stats collection",
		func(m *pebble.Metrics) float64 { return float64(m.Table.PendingStatsCollectionCount) })

	p.gauge("pebble_blob_files_live_count", "{count}", "Live blob file count",
		func(m *pebble.Metrics) float64 { return float64(m.BlobFiles.LiveCount) })
	p.gauge("pebble_blob_files_live_size", "By", "Live blob file physical size",
		func(m *pebble.Metrics) float64 { return float64(m.BlobFiles.LiveSize) })
	p.gauge("pebble_blob_files_value_size", "By", "Uncompressed value size in live blobs",
		func(m *pebble.Metrics) float64 { return float64(m.BlobFiles.ValueSize) })
	p.gauge("pebble_blob_files_referenced_value_size", "By", "Referenced value size in live blobs",
		func(m *pebble.Metrics) float64 { return float64(m.BlobFiles.ReferencedValueSize) })
	p.gauge("pebble_blob_files_obsolete_count", "{count}", "Obsolete blob file count",
		func(m *pebble.Metrics) float64 { return float64(m.BlobFiles.ObsoleteCount) })
	p.gauge("pebble_blob_files_obsolete_size", "By", "Obsolete blob file size",
		func(m *pebble.Metrics) float64 { return float64(m.BlobFiles.ObsoleteSize) })
	p.gauge("pebble_blob_files_zombie_count", "{count}", "Zombie blob file count",
		func(m *pebble.Metrics) float64 { return float64(m.BlobFiles.ZombieCount) })
	p.gauge("pebble_blob_files_zombie_size", "By", "Zombie blob file size",
		func(m *pebble.Metrics) float64 { return float64(m.BlobFiles.ZombieSize) })
	p.gauge("pebble_blob_files_local_live_size", "By", "Local live blob file size",
		func(m *pebble.Metrics) float64 { return float64(m.BlobFiles.Local.LiveSize) })
	p.gauge("pebble_blob_files_local_live_count", "{count}", "Local live blob file count",
		func(m *pebble.Metrics) float64 { return float64(m.BlobFiles.Local.LiveCount) })
	p.gauge("pebble_blob_files_local_obsolete_size", "By", "Local obsolete blob file size",
		func(m *pebble.Metrics) float64 { return float64(m.BlobFiles.Local.ObsoleteSize) })
	p.gauge("pebble_blob_files_local_obsolete_count", "{count}", "Local obsolete blob file count",
		func(m *pebble.Metrics) float64 { return float64(m.BlobFiles.Local.ObsoleteCount) })
	p.gauge("pebble_blob_files_local_zombie_size", "By", "Local zombie blob file size",
		func(m *pebble.Metrics) float64 { return float64(m.BlobFiles.Local.ZombieSize) })
	p.gauge("pebble_blob_files_local_zombie_count", "{count}", "Local zombie blob file count",
		func(m *pebble.Metrics) float64 { return float64(m.BlobFiles.Local.ZombieCount) })

	p.gauge("pebble_file_cache_size", "By", "Bytes in file cache",
		func(m *pebble.Metrics) float64 { return float64(m.FileCache.Size) })
	p.gauge("pebble_file_cache_table_count", "{count}", "Tables in file cache",
		func(m *pebble.Metrics) float64 { return float64(m.FileCache.TableCount) })
	p.gauge("pebble_file_cache_blob_file_count", "{count}", "Blob files in file cache",
		func(m *pebble.Metrics) float64 { return float64(m.FileCache.BlobFileCount) })
	p.counter("pebble_file_cache_hits", "{count}", "File cache hits",
		func(m *pebble.Metrics) float64 { return float64(m.FileCache.Hits) })
	p.counter("pebble_file_cache_misses", "{count}", "File cache misses",
		func(m *pebble.Metrics) float64 { return float64(m.FileCache.Misses) })

	p.gauge("pebble_num_virtual", "{count}", "Total virtual sstable count",
		func(m *pebble.Metrics) float64 { return float64(m.NumVirtual()) })
	p.gauge("pebble_virtual_size", "By", "Total virtual sstable size",
		func(m *pebble.Metrics) float64 { return float64(m.VirtualSize()) })
	p.gauge("pebble_remote_tables_count", "{count}", "Remote tables count",
		func(m *pebble.Metrics) float64 {
			count, _ := m.RemoteTablesTotal()
			return float64(count)
		})
	p.gauge("pebble_remote_tables_size", "By", "Remote tables size",
		func(m *pebble.Metrics) float64 {
			_, size := m.RemoteTablesTotal()
			return float64(size)
		})

	p.gauge("pebble_keys_range_key_sets_count", "{count}", "Approximate count of internal range key set keys",
		func(m *pebble.Metrics) float64 { return float64(m.Keys.RangeKeySetsCount) })
	p.gauge("pebble_keys_tombstone_count", "{count}", "Approximate count of internal tombstones",
		func(m *pebble.Metrics) float64 { return float64(m.Keys.TombstoneCount) })
	p.counter("pebble_keys_missized_tombstones_count", "{count}", "Missized DELSIZED keys encountered by compactions",
		func(m *pebble.Metrics) float64 { return float64(m.Keys.MissizedTombstonesCount) })

	p.gauge("pebble_snapshot_count", "{count}", "Number of currently open snapshots",
		func(m *pebble.Metrics) float64 { return float64(m.Snapshots.Count) })
	p.counter("pebble_snapshot_pinned_keys", "{count}", "Keys written that would've been elided without open snapshots",
		func(m *pebble.Metrics) float64 { return float64(m.Snapshots.PinnedKeys) })
	p.counter("pebble_snapshot_pinned_size", "By", "Size of keys/values written due to open snapshots",
		func(m *pebble.Metrics) float64 { return float64(m.Snapshots.PinnedSize) })
	p.gauge("pebble_snapshot_earliest_seq_num", "{count}", "Sequence number of earliest open snapshot",
		func(m *pebble.Metrics) float64 { return float64(m.Snapshots.EarliestSeqNum) })

	p.gauge("pebble_table_iters", "{count}", "Count of open sstable iterators",
		func(m *pebble.Metrics) float64 { return float64(m.TableIters) })
	p.gauge("pebble_uptime_seconds", "s", "Seconds since DB was opened",
		func(m *pebble.Metrics) float64 { return m.Uptime.Seconds() })
	p.gauge("pebble_read_amp", "{count}", "Read amplification",
		func(m *pebble.Metrics) float64 { return float64(m.ReadAmp()) })
	p.gauge("pebble_disk_space_usage", "By", "Total disk space used by the DB",
		func(m *pebble.Metrics) float64 { return float64(m.DiskSpaceUsage()) })

	p.counter("pebble_cache_hits", "{count}", "Total number of cache hits",
		func(m *pebble.Metrics) float64 { return float64(m.BlockCache.Hits) })
	p.counter("pebble_cache_misses", "{count}", "Total number of cache misses",
		func(m *pebble.Metrics) float64 { return float64(m.BlockCache.Misses) })
	p.gauge("pebble_cache_size", "By", "Current cache size",
		func(m *pebble.Metrics) float64 { return float64(m.BlockCache.Size) })
}

// declareLevels declares the series read per LSM level, each reported with a
// "level" attribute.
func (p *pebbleMetrics) declareLevels() {
	p.levelGauge("pebble_sstable_count", "{count}", "Current number of SSTables at each level",
		func(lm *pebble.LevelMetrics) float64 { return float64(lm.TablesCount) })
	p.levelGauge("pebble_sstable_total_size", "By", "Total size of SSTables at each level",
		func(lm *pebble.LevelMetrics) float64 { return float64(lm.TablesSize) })
	p.levelGauge("pebble_sstable_sublevels", "{count}", "Number of sublevels (read amplification); L0 only has non-0/1",
		func(lm *pebble.LevelMetrics) float64 { return float64(lm.Sublevels) })
	p.levelGauge("pebble_sstable_score", "1", "Level compaction score (0 if no compaction needed)",
		func(lm *pebble.LevelMetrics) float64 { return lm.Score })
	p.levelGauge("pebble_sstable_fill_factor", "1", "Level fill factor (size vs ideal size)",
		func(lm *pebble.LevelMetrics) float64 { return lm.FillFactor })
	p.levelGauge("pebble_sstable_virtual_count", "{count}", "Number of virtual sstables at level",
		func(lm *pebble.LevelMetrics) float64 { return float64(lm.VirtualTablesCount) })
	p.levelGauge("pebble_sstable_virtual_size", "By", "Size of virtual sstables at level",
		func(lm *pebble.LevelMetrics) float64 { return float64(lm.VirtualTablesSize) })
	p.levelCounter("pebble_compaction_bytes_read", "By", "Total bytes read during compaction",
		func(lm *pebble.LevelMetrics) float64 { return float64(lm.TableBytesIn) })
	p.levelCounter("pebble_compaction_bytes_written", "By", "Total bytes written during compaction",
		func(lm *pebble.LevelMetrics) float64 { return float64(lm.TableBytesCompacted) })
	p.levelCounter("pebble_sstable_bytes_ingested", "By", "Sstable bytes ingested at level",
		func(lm *pebble.LevelMetrics) float64 { return float64(lm.TableBytesIngested) })
	p.levelCounter("pebble_sstable_bytes_moved", "By", "Sstable bytes moved by move compaction at level",
		func(lm *pebble.LevelMetrics) float64 { return float64(lm.TableBytesMoved) })
	p.levelCounter("pebble_sstable_bytes_read", "By", "Bytes read for compactions at level",
		func(lm *pebble.LevelMetrics) float64 { return float64(lm.TableBytesRead) })
	p.levelCounter("pebble_sstable_bytes_flushed", "By", "Bytes written to sstables during flushes at level",
		func(lm *pebble.LevelMetrics) float64 { return float64(lm.TableBytesFlushed) })
	p.levelCounter("pebble_sstable_tables_compacted", "{count}", "Sstables compacted to this level",
		func(lm *pebble.LevelMetrics) float64 { return float64(lm.TablesCompacted) })
	p.levelCounter("pebble_sstable_tables_flushed", "{count}", "Sstables flushed to this level",
		func(lm *pebble.LevelMetrics) float64 { return float64(lm.TablesFlushed) })
	p.levelCounter("pebble_sstable_tables_ingested", "{count}", "Sstables ingested into level",
		func(lm *pebble.LevelMetrics) float64 { return float64(lm.TablesIngested) })
	p.levelCounter("pebble_sstable_tables_moved", "{count}", "Sstables moved to level by move compaction",
		func(lm *pebble.LevelMetrics) float64 { return float64(lm.TablesMoved) })
	p.levelGauge("pebble_sstable_compensated_fill_factor", "1", "Level compensated fill factor",
		func(lm *pebble.LevelMetrics) float64 { return lm.CompensatedFillFactor })
	p.levelGauge("pebble_sstable_estimated_references_size", "By", "Est. physical size of blob refs at level",
		func(lm *pebble.LevelMetrics) float64 { return float64(lm.EstimatedReferencesSize) })
	p.levelCounter("pebble_sstable_tables_deleted", "{count}", "Sstables deleted by delete-only compaction at level",
		func(lm *pebble.LevelMetrics) float64 { return float64(lm.TablesDeleted) })
	p.levelCounter("pebble_sstable_tables_excised", "{count}", "Sstables excised by delete-only compaction at level",
		func(lm *pebble.LevelMetrics) float64 { return float64(lm.TablesExcised) })
	p.levelCounter("pebble_sstable_blob_bytes_read_estimate", "By", "Est. physical bytes read for blob refs at level",
		func(lm *pebble.LevelMetrics) float64 { return float64(lm.BlobBytesReadEstimate) })
	p.levelCounter("pebble_sstable_blob_bytes_compacted", "By", "Blob bytes written during compaction at level",
		func(lm *pebble.LevelMetrics) float64 { return float64(lm.BlobBytesCompacted) })
	p.levelCounter("pebble_sstable_blob_bytes_flushed", "By", "Blob bytes written during flush at level",
		func(lm *pebble.LevelMetrics) float64 { return float64(lm.BlobBytesFlushed) })
	p.levelCounter("pebble_sstable_multi_level_bytes_in_top", "By", "Bytes from top level in multilevel compaction",
		func(lm *pebble.LevelMetrics) float64 { return float64(lm.MultiLevel.TableBytesInTop) })
	p.levelCounter("pebble_sstable_multi_level_bytes_in", "By", "Bytes in for multilevel compaction",
		func(lm *pebble.LevelMetrics) float64 { return float64(lm.MultiLevel.TableBytesIn) })
	p.levelCounter("pebble_sstable_multi_level_bytes_read", "By", "Bytes read for multilevel compaction",
		func(lm *pebble.LevelMetrics) float64 { return float64(lm.MultiLevel.TableBytesRead) })
	p.levelGauge("pebble_sstable_value_blocks_size", "By", "Value blocks size at level",
		func(lm *pebble.LevelMetrics) float64 { return float64(lm.Additional.ValueBlocksSize) })
	p.levelCounter("pebble_sstable_bytes_written_data_blocks", "By", "Bytes written to data blocks at level",
		func(lm *pebble.LevelMetrics) float64 { return float64(lm.Additional.BytesWrittenDataBlocks) })
	p.levelCounter("pebble_sstable_bytes_written_value_blocks", "By", "Bytes written to value blocks at level",
		func(lm *pebble.LevelMetrics) float64 { return float64(lm.Additional.BytesWrittenValueBlocks) })
}
