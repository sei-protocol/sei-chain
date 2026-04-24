package migration

import (
	"context"
	"sync"
	"time"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/metric"

	commonmetrics "github.com/sei-protocol/sei-chain/sei-db/common/metrics"
)

// MigrationMetrics holds OpenTelemetry metrics for a MigrationManager.
// Metrics are exported via whatever exporter is configured on the global
// OTel MeterProvider. All methods are nil-safe so callers (and tests)
// that do not care about metrics can pass a nil *MigrationMetrics to the
// manager.
//
// Unit convention: durations in "s", bytes in "By", counts via curly-brace
// annotations (UCUM, https://ucum.org/ucum).
type MigrationMetrics struct {
	ctx    context.Context
	cancel context.CancelFunc

	// wg tracks the background boundary-snapshot goroutine (if any) so
	// Close can block until it exits.
	wg sync.WaitGroup

	// targetVersion is captured at construction time so the boundary
	// snapshot goroutine can tell, without any DB access, when the
	// migration has completed and it can stop emitting labeled series.
	targetVersion uint64

	keysMigratedTotal       metric.Int64Counter
	keyBytesMigratedTotal   metric.Int64Counter
	valueBytesMigratedTotal metric.Int64Counter
	applyDuration           metric.Float64Histogram
	version                 metric.Int64Gauge
	boundarySnapshot        metric.Int64Gauge

	mu              sync.Mutex
	currentBoundary MigrationBoundary
	currentVersion  uint64
}

// NewMigrationMetrics constructs a MigrationMetrics using the global OTel
// MeterProvider. The caller must have configured the MeterProvider with a
// Prometheus or other exporter before calling this.
//
// targetVersion is the version the associated migration is transitioning
// to; it is used solely by the boundary-snapshot goroutine to decide when
// to stop emitting labeled series.
//
// When boundarySnapshotInterval <= 0 the snapshot goroutine is not started;
// everything else still works. When ctx is cancelled, or Close is called,
// the snapshot goroutine exits.
func NewMigrationMetrics(
	ctx context.Context,
	targetVersion uint64,
	boundarySnapshotInterval time.Duration,
) *MigrationMetrics {
	ctx, cancel := context.WithCancel(ctx)
	meter := otel.Meter("seidb_migration")

	keysMigratedTotal, _ := meter.Int64Counter(
		"seidb_migration_keys_migrated_total",
		metric.WithDescription("Total number of keys promoted from the old DB to the new DB"),
		metric.WithUnit("{count}"),
	)
	keyBytesMigratedTotal, _ := meter.Int64Counter(
		"seidb_migration_key_bytes_migrated_total",
		metric.WithDescription("Running sum of bytes of migrated keys (len(Key))"),
		metric.WithUnit("By"),
	)
	valueBytesMigratedTotal, _ := meter.Int64Counter(
		"seidb_migration_value_bytes_migrated_total",
		metric.WithDescription("Running sum of bytes of migrated values (len(Value))"),
		metric.WithUnit("By"),
	)
	applyDuration, _ := meter.Float64Histogram(
		"seidb_migration_apply_change_sets_duration_seconds",
		metric.WithDescription("Wall-clock time spent in each MigrationManager.ApplyChangeSets call"),
		metric.WithUnit("s"),
		metric.WithExplicitBucketBoundaries(commonmetrics.LatencyBuckets...),
	)
	version, _ := meter.Int64Gauge(
		"seidb_migration_version",
		metric.WithDescription("Currently-observed migration version. Equals targetVersion once the "+
			"migration is complete, startVersion while in progress."),
		metric.WithUnit("{version}"),
	)
	boundarySnapshot, _ := meter.Int64Gauge(
		"seidb_migration_boundary_snapshot",
		metric.WithDescription("Periodic snapshot of the live migration boundary. Value is always 1; inspect the "+
			"boundary_hex label for the boundary itself."),
		metric.WithUnit("{boundary}"),
	)

	m := &MigrationMetrics{
		ctx:                     ctx,
		cancel:                  cancel,
		targetVersion:           targetVersion,
		keysMigratedTotal:       keysMigratedTotal,
		keyBytesMigratedTotal:   keyBytesMigratedTotal,
		valueBytesMigratedTotal: valueBytesMigratedTotal,
		applyDuration:           applyDuration,
		version:                 version,
		boundarySnapshot:        boundarySnapshot,
	}

	if boundarySnapshotInterval > 0 {
		m.startBoundarySnapshotLoop(boundarySnapshotInterval)
	}
	return m
}

// SetBoundary updates the in-memory current boundary. No DB access. Safe
// to call concurrently with the snapshot ticker; not safe to call
// concurrently with itself from multiple goroutines, but the
// MigrationManager only updates the boundary from a single ApplyChangeSets
// caller at a time.
func (m *MigrationMetrics) SetBoundary(b MigrationBoundary) {
	if m == nil {
		return
	}
	m.mu.Lock()
	m.currentBoundary = b
	m.mu.Unlock()
}

// SetVersion updates the in-memory current migration version and records
// the version gauge immediately so Grafana sees the transition without
// waiting for the next snapshot tick.
func (m *MigrationMetrics) SetVersion(v uint64) {
	if m == nil {
		return
	}
	m.mu.Lock()
	m.currentVersion = v
	m.mu.Unlock()
	if m.version != nil {
		m.version.Record(context.Background(), int64(v)) //nolint:gosec // version is monotonic and bounded
	}
}

// ReportKeysMigrated records that a batch of (count) keys totaling
// (keyBytes, valueBytes) were migrated in a single ApplyChangeSets call.
// Pass zeros to skip; the method is a no-op on nil receiver.
func (m *MigrationMetrics) ReportKeysMigrated(count int64, keyBytes int64, valueBytes int64) {
	if m == nil {
		return
	}
	ctx := context.Background()
	if m.keysMigratedTotal != nil && count > 0 {
		m.keysMigratedTotal.Add(ctx, count)
	}
	if m.keyBytesMigratedTotal != nil && keyBytes > 0 {
		m.keyBytesMigratedTotal.Add(ctx, keyBytes)
	}
	if m.valueBytesMigratedTotal != nil && valueBytes > 0 {
		m.valueBytesMigratedTotal.Add(ctx, valueBytes)
	}
}

// RecordApplyDuration records the wall-clock time spent in a single
// ApplyChangeSets call. Invoked from a defer in the manager so both
// success and error paths are captured.
func (m *MigrationMetrics) RecordApplyDuration(d time.Duration) {
	if m == nil || m.applyDuration == nil {
		return
	}
	m.applyDuration.Record(context.Background(), d.Seconds())
}

// snapshot returns a safe copy of the in-memory boundary and version
// under the mutex. The returned boundary shares its internal key slice
// with the stored value; callers must not mutate it.
func (m *MigrationMetrics) snapshot() (MigrationBoundary, uint64) {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.currentBoundary, m.currentVersion
}

// startBoundarySnapshotLoop starts the background goroutine that
// periodically emits the labeled boundary snapshot gauge. The loop exits
// on ctx cancellation, or after emitting a single "complete" sentinel
// once currentVersion reaches targetVersion.
//
// Cardinality rationale: at a 10-minute interval a month-long migration
// tops out at ~4k unique boundary_hex label values — well within
// Prometheus' comfort zone — and the OTel exporter's staleness markers
// keep only the most recent label active in the scrape set.
func (m *MigrationMetrics) startBoundarySnapshotLoop(interval time.Duration) {
	if m == nil || m.boundarySnapshot == nil {
		return
	}
	m.wg.Add(1)
	go func() {
		defer m.wg.Done()
		ticker := time.NewTicker(interval)
		defer ticker.Stop()
		completedEmitted := false
		for {
			select {
			case <-m.ctx.Done():
				return
			case <-ticker.C:
				boundary, version := m.snapshot()
				if version == m.targetVersion {
					if !completedEmitted {
						m.recordBoundarySnapshot("complete")
					}
					return
				}
				m.recordBoundarySnapshot(boundary.String())
			}
		}
	}()
}

// Release resources held by the metrics collector.
func (m *MigrationMetrics) Close() {
	if m == nil {
		return
	}
	m.cancel()
	m.wg.Wait()
}

// recordBoundarySnapshot emits the labeled snapshot gauge with value 1.
// The label is the only payload — the value itself is unused.
func (m *MigrationMetrics) recordBoundarySnapshot(label string) {
	m.boundarySnapshot.Record(
		context.Background(),
		1,
		metric.WithAttributes(attribute.String("boundary_hex", label)),
	)
}
