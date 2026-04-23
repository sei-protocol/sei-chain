package migration

import (
	"context"
	"testing"
	"time"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	sdkmetric "go.opentelemetry.io/otel/sdk/metric"
	"go.opentelemetry.io/otel/sdk/metric/metricdata"

	"github.com/stretchr/testify/require"
)

// installTestMeterProvider wires up a fresh SDK MeterProvider with a
// ManualReader on the global OTel registry so tests can call
// NewMigrationMetrics (which goes through otel.Meter) and then read the
// metrics back synchronously. The previous provider is restored on cleanup.
func installTestMeterProvider(t *testing.T) *sdkmetric.ManualReader {
	t.Helper()
	reader := sdkmetric.NewManualReader()
	provider := sdkmetric.NewMeterProvider(sdkmetric.WithReader(reader))
	prev := otel.GetMeterProvider()
	otel.SetMeterProvider(provider)
	t.Cleanup(func() {
		_ = provider.Shutdown(context.Background())
		otel.SetMeterProvider(prev)
	})
	return reader
}

// collect flushes the ManualReader once and returns the result.
func collect(t *testing.T, reader *sdkmetric.ManualReader) metricdata.ResourceMetrics {
	t.Helper()
	var rm metricdata.ResourceMetrics
	require.NoError(t, reader.Collect(context.Background(), &rm))
	return rm
}

// findMetric walks all scopes looking for a metric with the given name.
// Returns nil if not present.
func findMetric(rm metricdata.ResourceMetrics, name string) *metricdata.Metrics {
	for i := range rm.ScopeMetrics {
		for j := range rm.ScopeMetrics[i].Metrics {
			if rm.ScopeMetrics[i].Metrics[j].Name == name {
				return &rm.ScopeMetrics[i].Metrics[j]
			}
		}
	}
	return nil
}

// int64GaugeValue returns the sole data point of an Int64 gauge, asserting
// the metric is present and has exactly one data point.
func int64GaugeValue(t *testing.T, rm metricdata.ResourceMetrics, name string) int64 {
	t.Helper()
	m := findMetric(rm, name)
	require.NotNil(t, m, "metric %q missing", name)
	g, ok := m.Data.(metricdata.Gauge[int64])
	require.True(t, ok, "metric %q is not an Int64 gauge (got %T)", name, m.Data)
	require.Len(t, g.DataPoints, 1, "metric %q should have exactly one data point", name)
	return g.DataPoints[0].Value
}

// int64CounterValue returns the sum of the sole data point of an Int64
// counter.
func int64CounterValue(t *testing.T, rm metricdata.ResourceMetrics, name string) int64 {
	t.Helper()
	m := findMetric(rm, name)
	require.NotNil(t, m, "metric %q missing", name)
	s, ok := m.Data.(metricdata.Sum[int64])
	require.True(t, ok, "metric %q is not an Int64 sum (got %T)", name, m.Data)
	require.Len(t, s.DataPoints, 1, "metric %q should have exactly one data point", name)
	return s.DataPoints[0].Value
}

// boundarySnapshotLabels returns every boundary_hex label value ever
// recorded on the boundary snapshot gauge, in arbitrary order.
func boundarySnapshotLabels(t *testing.T, rm metricdata.ResourceMetrics) []string {
	t.Helper()
	m := findMetric(rm, "seidb_migration_boundary_snapshot")
	if m == nil {
		return nil
	}
	g, ok := m.Data.(metricdata.Gauge[int64])
	require.True(t, ok, "boundary snapshot must be an Int64 gauge")
	labels := make([]string, 0, len(g.DataPoints))
	for _, dp := range g.DataPoints {
		if v, ok := dp.Attributes.Value(attribute.Key("boundary_hex")); ok {
			labels = append(labels, v.AsString())
		}
	}
	return labels
}

// --- Nil safety ---

func TestMigrationMetrics_NilReceiverIsSafe(t *testing.T) {
	var m *MigrationMetrics
	require.NotPanics(t, func() {
		m.SetBoundary(NewMigrationBoundary("bank", []byte("a")))
		m.SetVersion(7)
		m.ReportKeysMigrated(10, 100, 1000)
		m.RecordApplyDuration(5 * time.Millisecond)
	})
}

// --- SetVersion records immediately ---

func TestMigrationMetrics_SetVersionRecordsGauge(t *testing.T) {
	reader := installTestMeterProvider(t)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	m := NewMigrationMetrics(ctx, 7, 0)

	m.SetVersion(3)
	require.Equal(t, int64(3), int64GaugeValue(t, collect(t, reader), "seidb_migration_version"))

	m.SetVersion(7)
	require.Equal(t, int64(7), int64GaugeValue(t, collect(t, reader), "seidb_migration_version"))
}

// --- ReportKeysMigrated advances counters ---

func TestMigrationMetrics_ReportKeysMigratedAdvancesCounters(t *testing.T) {
	reader := installTestMeterProvider(t)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	m := NewMigrationMetrics(ctx, 1, 0)

	m.ReportKeysMigrated(3, 30, 300)
	m.ReportKeysMigrated(2, 20, 200)

	rm := collect(t, reader)
	require.Equal(t, int64(5), int64CounterValue(t, rm, "seidb_migration_keys_migrated_total"))
	require.Equal(t, int64(50), int64CounterValue(t, rm, "seidb_migration_key_bytes_migrated_total"))
	require.Equal(t, int64(500), int64CounterValue(t, rm, "seidb_migration_value_bytes_migrated_total"))
}

// --- Boundary snapshot ticker ---

func TestMigrationMetrics_BoundarySnapshotEmitsLabeledValue(t *testing.T) {
	reader := installTestMeterProvider(t)

	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)

	m := NewMigrationMetrics(ctx, 2, 5*time.Millisecond)
	m.SetVersion(1)
	m.SetBoundary(NewMigrationBoundary("bank", []byte("a")))

	require.Eventually(t, func() bool {
		labels := boundarySnapshotLabels(t, collect(t, reader))
		for _, l := range labels {
			if l != boundarySnapshotCompleteLabel {
				return true
			}
		}
		return false
	}, time.Second, 5*time.Millisecond, "expected boundary snapshot to be emitted")

	// Cancelling the ctx must stop further emissions. Give the goroutine
	// a moment to exit.
	cancel()
	time.Sleep(50 * time.Millisecond)
}

func TestMigrationMetrics_BoundarySnapshotStopsAfterCompletion(t *testing.T) {
	reader := installTestMeterProvider(t)

	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)

	// Small interval so the test completes quickly; targetVersion
	// reached immediately so the loop emits the complete sentinel and
	// exits on the first tick.
	m := NewMigrationMetrics(ctx, 1, 5*time.Millisecond)
	m.SetVersion(1)
	m.SetBoundary(MigrationBoundaryComplete)

	require.Eventually(t, func() bool {
		for _, l := range boundarySnapshotLabels(t, collect(t, reader)) {
			if l == boundarySnapshotCompleteLabel {
				return true
			}
		}
		return false
	}, time.Second, 5*time.Millisecond, "expected complete sentinel to be emitted")

	// Wait through multiple ticks; no new labels should appear. This is
	// an admittedly loose bound, but it confirms the ticker exited
	// rather than continuing to stamp "complete" forever.
	time.Sleep(50 * time.Millisecond)
	labels := boundarySnapshotLabels(t, collect(t, reader))
	count := 0
	for _, l := range labels {
		if l == boundarySnapshotCompleteLabel {
			count++
		}
	}
	require.Equal(t, 1, count, "complete sentinel should be emitted exactly once")
}

// --- End-to-end: manager drives all the metrics via ApplyChangeSets ---

func TestMigrationManager_MetricsIntegration(t *testing.T) {
	reader := installTestMeterProvider(t)

	data := map[string]map[string][]byte{
		"bank": {"a": []byte("1"), "bb": []byte("22")},
	}
	oldDB := newMockDB()
	oldDB.seed(copyData(data))
	newDB := newMockDB()

	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)
	metrics := NewMigrationMetrics(ctx, testTargetVersion, 0)

	mgr, err := NewMigrationManager(
		2,
		testStartVersion, testTargetVersion,
		oldDB.reader(), oldDB.writer(),
		newDB.reader(), newDB.writer(),
		NewMapMigrationIterator(copyData(data), false),
		metrics,
	)
	require.NoError(t, err)

	require.Equal(t, int64(testStartVersion),
		int64GaugeValue(t, collect(t, reader), "seidb_migration_version"),
		"version gauge should reflect startVersion on construction")

	require.NoError(t, mgr.ApplyChangeSets(context.Background(), nil))
	require.True(t, mgr.migrationFinished)

	rm := collect(t, reader)

	// Two keys migrated: "a" (1 byte key, 1 byte value) and "bb"
	// (2 byte key, 2 byte value) → 3 key-bytes, 3 value-bytes.
	require.Equal(t, int64(2), int64CounterValue(t, rm, "seidb_migration_keys_migrated_total"))
	require.Equal(t, int64(3), int64CounterValue(t, rm, "seidb_migration_key_bytes_migrated_total"))
	require.Equal(t, int64(3), int64CounterValue(t, rm, "seidb_migration_value_bytes_migrated_total"))

	// Version flipped to targetVersion on the final block.
	require.Equal(t, int64(testTargetVersion),
		int64GaugeValue(t, rm, "seidb_migration_version"),
		"version gauge should flip to targetVersion on the final block")

	// Duration histogram must have exactly one observation from the
	// single ApplyChangeSets call.
	durMetric := findMetric(rm, "seidb_migration_apply_change_sets_duration_seconds")
	require.NotNil(t, durMetric)
	hist, ok := durMetric.Data.(metricdata.Histogram[float64])
	require.True(t, ok, "duration metric must be a Float64 histogram (got %T)", durMetric.Data)
	require.Len(t, hist.DataPoints, 1)
	require.Equal(t, uint64(1), hist.DataPoints[0].Count)
}

// Sanity check: a nil metrics argument must not disturb the existing
// manager behaviour. The rest of the test suite relies on this being
// safe; this test pins it down with an explicit assertion.
func TestMigrationManager_NilMetricsIsSafe(t *testing.T) {
	data := map[string]map[string][]byte{"bank": {"a": []byte("1")}}
	oldDB := newMockDB()
	oldDB.seed(copyData(data))
	newDB := newMockDB()

	mgr, err := NewMigrationManager(
		10,
		testStartVersion, testTargetVersion,
		oldDB.reader(), oldDB.writer(),
		newDB.reader(), newDB.writer(),
		NewMapMigrationIterator(copyData(data), false),
		nil,
	)
	require.NoError(t, err)

	require.NoError(t, mgr.ApplyChangeSets(context.Background(), nil))
	require.True(t, mgr.migrationFinished)
}
